package volumegroup_sidecar_controller

import (
	v1alpha13 "github.com/deepakkinni/volumegroup-controller/pkg/apis/volumegroup/v1alpha1"
	v13 "k8s.io/api/core/v1"
	"time"

	clientset "github.com/deepakkinni/volumegroup-controller/pkg/client/clientset/versioned"
	v1alpha12 "github.com/deepakkinni/volumegroup-controller/pkg/client/informers/externalversions/volumegroup/v1alpha1"
	"github.com/deepakkinni/volumegroup-controller/pkg/client/listers/volumegroup/v1alpha1"
	"github.com/deepakkinni/volumegroup-controller/pkg/utils"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	v12 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type csiVolumeGroupSideCarController struct {
	clientset clientset.Interface
	client    kubernetes.Interface

	driverName   string
	resyncPeriod time.Duration

	volumeGroupQueue        workqueue.RateLimitingInterface
	volumeGroupContentQueue workqueue.RateLimitingInterface
	volumeGroupClassQueue   workqueue.RateLimitingInterface
	pvcQueue                workqueue.RateLimitingInterface

	volumeGroupLister              v1alpha1.VolumeGroupLister
	volumeGroupListerSynced        cache.InformerSynced
	volumeGroupContentLister       v1alpha1.VolumeGroupContentLister
	volumeGroupContentListerSynced cache.InformerSynced
	volumeGroupClassLister         v1alpha1.VolumeGroupClassLister
	volumeGroupClassListerSynced   cache.InformerSynced
	pvcLister                      v1.PersistentVolumeClaimLister
	pvcListerSynced                cache.InformerSynced

	volumeGroupStore        cache.Store
	volumeGroupContentStore cache.Store
	volumeGroupClassStore   cache.Store
	pvcStore                cache.Store
}

func NewCSIVolumeGroupSideCarController(
	clientset clientset.Interface,
	client kubernetes.Interface,
	driverName string,
	volumeGroupInformer v1alpha12.VolumeGroupInformer,
	volumeGroupContentInformer v1alpha12.VolumeGroupContentInformer,
	volumeGroupClassInformer v1alpha12.VolumeGroupClassInformer,
	pvcInformer v12.PersistentVolumeClaimInformer,
	resyncPeriod time.Duration,
	volumeGroupRateLimiter workqueue.RateLimiter,
	volumeGroupContentRateLimiter workqueue.RateLimiter,
	volumeGroupClassRateLimiter workqueue.RateLimiter,
	pvcRateLimiter workqueue.RateLimiter,
) *csiVolumeGroupSideCarController {

	ctrl := &csiVolumeGroupSideCarController{
		clientset:               clientset,
		client:                  client,
		driverName:              driverName,
		volumeGroupQueue:        workqueue.NewNamedRateLimitingQueue(volumeGroupRateLimiter, "volumegroup-controller-volumegroup"),
		volumeGroupContentQueue: workqueue.NewNamedRateLimitingQueue(volumeGroupContentRateLimiter, "volumegroup-controller-volumegroupcontent"),
		volumeGroupClassQueue:   workqueue.NewNamedRateLimitingQueue(volumeGroupClassRateLimiter, "volumegroup-controller-volumegroupclass"),
		pvcQueue:                workqueue.NewNamedRateLimitingQueue(pvcRateLimiter, "volumegroup-controller-pvc"),
		volumeGroupStore:        cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
		volumeGroupContentStore: cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
		volumeGroupClassStore:   cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
		pvcStore:                cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
		resyncPeriod:            resyncPeriod,
	}

	ctrl.volumeGroupLister = volumeGroupInformer.Lister()
	ctrl.volumeGroupListerSynced = volumeGroupInformer.Informer().HasSynced

	volumeGroupInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {ctrl.enqueueVolumeGroupWork(obj)},
			UpdateFunc: func(oldObj, newObj interface{}) {ctrl.enqueueVolumeGroupWork(newObj)},
			DeleteFunc: func(obj interface{}) {ctrl.enqueueVolumeGroupWork(obj)},
		},
		ctrl.resyncPeriod,
	)

	ctrl.volumeGroupContentLister = volumeGroupContentInformer.Lister()
	ctrl.volumeGroupContentListerSynced = volumeGroupContentInformer.Informer().HasSynced

	volumeGroupContentInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {ctrl.enqueueVolumeGroupContentWork(obj)},
			UpdateFunc: func(oldObj, newObj interface{}) {ctrl.enqueueVolumeGroupContentWork(newObj)},
			DeleteFunc: func(obj interface{}) {ctrl.enqueueVolumeGroupContentWork(obj)},
		},
		ctrl.resyncPeriod,
	)

	ctrl.volumeGroupClassLister = volumeGroupClassInformer.Lister()
	ctrl.volumeGroupClassListerSynced = volumeGroupClassInformer.Informer().HasSynced

	volumeGroupClassInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {ctrl.enqueueVolumeClassGroupWork(obj)},
			UpdateFunc: func(oldObj, newObj interface{}) {ctrl.enqueueVolumeClassGroupWork(newObj)},
			DeleteFunc: func(obj interface{}) {ctrl.enqueueVolumeClassGroupWork(obj)},
		},
		ctrl.resyncPeriod,
	)

	ctrl.pvcLister = pvcInformer.Lister()
	ctrl.pvcListerSynced = pvcInformer.Informer().HasSynced

	pvcInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {ctrl.enqueuePvcWork(obj)},
			UpdateFunc: func(oldObj, newObj interface{}) {ctrl.enqueuePvcWork(newObj)},
			DeleteFunc: func(obj interface{}) {ctrl.enqueuePvcWork(obj)},
		},
		ctrl.resyncPeriod,
	)
	return ctrl
}

func (ctrl *csiVolumeGroupSideCarController) Run(workers int, stopCh <-chan struct{}) {
	defer ctrl.volumeGroupQueue.ShutDown()
	defer ctrl.volumeGroupContentQueue.ShutDown()
	defer ctrl.volumeGroupClassQueue.ShutDown()
	defer ctrl.pvcQueue.ShutDown()

	klog.Infof("Starting volume-group controller")
	defer klog.Infof("Shutting volume-group controller")

	informersSynced := []cache.InformerSynced{ctrl.volumeGroupListerSynced, ctrl.volumeGroupContentListerSynced,
		ctrl.volumeGroupClassListerSynced, ctrl.pvcListerSynced}

	if !cache.WaitForCacheSync(stopCh, informersSynced...) {
		klog.Errorf("Cannot sync caches")
		return
	}

	ctrl.initializeCaches(ctrl.volumeGroupLister, ctrl.volumeGroupContentLister,
		ctrl.volumeGroupClassLister, ctrl.pvcLister)

	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.volumeGroupWorker, 0, stopCh)
		go wait.Until(ctrl.volumeGroupContentWorker, 0, stopCh)
		go wait.Until(ctrl.volumeGroupClassWorker, 0, stopCh)
		go wait.Until(ctrl.pvcWorker, 0, stopCh)
	}

	<-stopCh
}

func (ctrl *csiVolumeGroupSideCarController) volumeGroupWorker() {
	keyObj, quit := ctrl.volumeGroupQueue.Get()
	if quit {
		return
	}
	defer ctrl.volumeGroupQueue.Done(keyObj)

	if err := ctrl.syncVolumeGroupByKey(keyObj.(string)); err != nil {
		// Rather than wait for a full resync, re-add the key to the
		// queue to be processed.
		ctrl.volumeGroupQueue.AddRateLimited(keyObj)
		klog.V(4).Infof("Failed to sync volumegroup %q, will retry again: %v", keyObj.(string), err)
	} else {
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		ctrl.volumeGroupQueue.Forget(keyObj)
	}
}

func (ctrl *csiVolumeGroupSideCarController) volumeGroupContentWorker() {
	keyObj, quit := ctrl.volumeGroupContentQueue.Get()
	if quit {
		return
	}
	defer ctrl.volumeGroupContentQueue.Done(keyObj)

	if err := ctrl.syncVolumeGroupContentByKey(keyObj.(string)); err != nil {
		// Rather than wait for a full resync, re-add the key to the
		// queue to be processed.
		ctrl.volumeGroupContentQueue.AddRateLimited(keyObj)
		klog.V(4).Infof("Failed to sync volumegroupcontent %q, will retry again: %v", keyObj.(string), err)
	} else {
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		ctrl.volumeGroupContentQueue.Forget(keyObj)
	}
}

func (ctrl *csiVolumeGroupSideCarController) volumeGroupClassWorker() {
	keyObj, quit := ctrl.volumeGroupClassQueue.Get()
	if quit {
		return
	}
	defer ctrl.volumeGroupClassQueue.Done(keyObj)

	if err := ctrl.syncVolumeGroupClassByKey(keyObj.(string)); err != nil {
		// Rather than wait for a full resync, re-add the key to the
		// queue to be processed.
		ctrl.volumeGroupClassQueue.AddRateLimited(keyObj)
		klog.V(4).Infof("Failed to sync volumegroupclass %q, will retry again: %v", keyObj.(string), err)
	} else {
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		ctrl.volumeGroupClassQueue.Forget(keyObj)
	}
}

func (ctrl *csiVolumeGroupSideCarController) pvcWorker() {
	keyObj, quit := ctrl.pvcQueue.Get()
	if quit {
		return
	}
	defer ctrl.pvcQueue.Done(keyObj)

	if err := ctrl.syncPvcByKey(keyObj.(string)); err != nil {
		// Rather than wait for a full resync, re-add the key to the
		// queue to be processed.
		ctrl.pvcQueue.AddRateLimited(keyObj)
		klog.V(4).Infof("Failed to sync pvc %q, will retry again: %v", keyObj.(string), err)
	} else {
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		ctrl.pvcQueue.Forget(keyObj)
	}
}

// enqueueVolumeGroupWork adds volumegroup to given work queue.
func (ctrl *csiVolumeGroupSideCarController) enqueueVolumeGroupWork(obj interface{}) {
	// Beware of "xxx deleted" events
	if unknown, ok := obj.(cache.DeletedFinalStateUnknown); ok && unknown.Obj != nil {
		obj = unknown.Obj
	}
	if volumeGroup, ok := obj.(*v1alpha13.VolumeGroup); ok {
		objName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(volumeGroup)
		if err != nil {
			klog.Errorf("failed to get key from object: %v, %v", err, volumeGroup)
			return
		}
		klog.V(5).Infof("enqueued %q for sync", objName)
		ctrl.volumeGroupQueue.Add(objName)
	}
}

// enqueueVolumeGroupContentWork adds volumegroupcontent to given work queue.
func (ctrl *csiVolumeGroupSideCarController) enqueueVolumeGroupContentWork(obj interface{}) {
	// Beware of "xxx deleted" events
	if unknown, ok := obj.(cache.DeletedFinalStateUnknown); ok && unknown.Obj != nil {
		obj = unknown.Obj
	}
	if volumeGroupContent, ok := obj.(*v1alpha13.VolumeGroupContent); ok {
		objName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(volumeGroupContent)
		if err != nil {
			klog.Errorf("failed to get key from object: %v, %v", err, volumeGroupContent)
			return
		}
		klog.V(5).Infof("enqueued %q for sync", objName)
		ctrl.volumeGroupContentQueue.Add(objName)
	}
}

// enqueueVolumeClassGroupWork adds volumegroupclass to given work queue.
func (ctrl *csiVolumeGroupSideCarController) enqueueVolumeClassGroupWork(obj interface{}) {
	// Beware of "xxx deleted" events
	if unknown, ok := obj.(cache.DeletedFinalStateUnknown); ok && unknown.Obj != nil {
		obj = unknown.Obj
	}
	if volumeGroupClass, ok := obj.(*v1alpha13.VolumeGroupClass); ok {
		objName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(volumeGroupClass)
		if err != nil {
			klog.Errorf("failed to get key from object: %v, %v", err, volumeGroupClass)
			return
		}
		klog.V(5).Infof("enqueued %q for sync", objName)
		ctrl.volumeGroupClassQueue.Add(objName)
	}
}

// enqueuePvcWork adds pvc to given work queue.
func (ctrl *csiVolumeGroupSideCarController) enqueuePvcWork(obj interface{}) {
	// Beware of "xxx deleted" events
	if unknown, ok := obj.(cache.DeletedFinalStateUnknown); ok && unknown.Obj != nil {
		obj = unknown.Obj
	}
	if pvc, ok := obj.(*v13.PersistentVolumeClaim); ok {
		objName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(pvc)
		if err != nil {
			klog.Errorf("failed to get key from object: %v, %v", err, pvc)
			return
		}
		klog.V(5).Infof("enqueued %q for sync", objName)
		ctrl.pvcQueue.Add(objName)
	}
}

// initializeCaches fills all controller caches with initial data from etcd
func (ctrl *csiVolumeGroupSideCarController) initializeCaches(volumeGroupLister v1alpha1.VolumeGroupLister,
	volumeGroupContentLister v1alpha1.VolumeGroupContentLister,
	volumeGroupClassLister v1alpha1.VolumeGroupClassLister,
	pvcLister v1.PersistentVolumeClaimLister) {

	volumeGroupList, err := volumeGroupLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("CSIVolumeGroupSideCarController can't initialize caches: %v", err)
		return
	}
	for _, volumeGroup := range volumeGroupList {
		volumeGroupClone := volumeGroup.DeepCopy()
		if _, err = ctrl.storeVolumeGroupUpdate(volumeGroupClone); err != nil {
			klog.Errorf("error updating volumegroup cache: %v", err)
		}
	}

	volumeGroupContentList, err := volumeGroupContentLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("CSIVolumeGroupSideCarController can't initialize caches: %v", err)
		return
	}
	for _, content := range volumeGroupContentList {
		contentClone := content.DeepCopy()
		if _, err = ctrl.storeVolumeGroupContentUpdate(contentClone); err != nil {
			klog.Errorf("error updating volumegroupcontent cache: %v", err)
		}
	}

	volumeGroupClassList, err := volumeGroupClassLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("CSIVolumeGroupSideCarController can't initialize caches: %v", err)
		return
	}
	for _, class := range volumeGroupClassList {
		classClone := class.DeepCopy()
		if _, err = ctrl.storeVolumeGroupClassUpdate(classClone); err != nil {
			klog.Errorf("error updating volumegroupclass cache: %v", err)
		}
	}

	pvcList, err := pvcLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("CSIVolumeGroupSideCarController can't initialize caches: %v", err)
		return
	}
	for _, pvc := range pvcList {
		pvcClone := pvc.DeepCopy()
		if _, err = ctrl.storePVCUpdate(pvcClone); err != nil {
			klog.Errorf("error updating pvc cache: %v", err)
		}
	}

	klog.V(4).Infof("controller initialized")
}

func (ctrl *csiVolumeGroupSideCarController) storeVolumeGroupUpdate(volumeGroup interface{}) (bool, error) {
	return utils.StoreObjectUpdate(ctrl.volumeGroupStore, volumeGroup, "volumegroup")
}

func (ctrl *csiVolumeGroupSideCarController) storeVolumeGroupContentUpdate(volumeGroupContent interface{}) (bool, error) {
	return utils.StoreObjectUpdate(ctrl.volumeGroupContentStore, volumeGroupContent, "volumegroupcontent")
}

func (ctrl *csiVolumeGroupSideCarController) storeVolumeGroupClassUpdate(volumeGroupClass interface{}) (bool, error) {
	return utils.StoreObjectUpdate(ctrl.volumeGroupClassStore, volumeGroupClass, "volumegroupclass")
}

func (ctrl *csiVolumeGroupSideCarController) storePVCUpdate(pvc interface{}) (bool, error) {
	return utils.StoreObjectUpdate(ctrl.pvcStore, pvc, "pvc")
}

// syncVolumeGroupByKey processes a VolumeGroup request.
func (ctrl *csiVolumeGroupSideCarController) syncVolumeGroupByKey(key string) error {
	klog.V(5).Infof("syncVolumeGroupByKey[%s]", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.V(5).Infof("volumeGroupWorker: volumegroup namespace [%s] name [%s]", namespace, name)
	if err != nil {
		klog.Errorf("error getting namespace & name of volumegroup %q to get volumegroup from informer: %v", key, err)
		return nil
	}
	return nil
}

// syncVolumeGroupContentByKey processes a VolumeGroupContent request.
func (ctrl *csiVolumeGroupSideCarController) syncVolumeGroupContentByKey(key string) error {
	klog.V(5).Infof("syncVolumeGroupByKey[%s]", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.V(5).Infof("volumeGroupContentWorker: volumegroupcontent namespace [%s] name [%s]", namespace, name)
	if err != nil {
		klog.Errorf("error getting namespace & name of volumegroupcontent %q to get volumegroupcontent from informer: %v", key, err)
		return nil
	}
	return nil
}

// syncVolumeGroupClassByKey processes a VolumeGroupClass request.
func (ctrl *csiVolumeGroupSideCarController) syncVolumeGroupClassByKey(key string) error {
	klog.V(5).Infof("syncVolumeGroupClassByKey[%s]", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.V(5).Infof("volumeGroupClassWorker: volumegroupclass namespace [%s] name [%s]", namespace, name)
	if err != nil {
		klog.Errorf("error getting namespace & name of volumegroupclass %q to get volumegroupclass from informer: %v", key, err)
		return nil
	}
	return nil
}

// syncPvcByKey processes a PVC request.
func (ctrl *csiVolumeGroupSideCarController) syncPvcByKey(key string) error {
	klog.V(5).Infof("syncPvcByKey[%s]", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.V(5).Infof("pvcWorker: pvc namespace [%s] name [%s]", namespace, name)
	if err != nil {
		klog.Errorf("error getting namespace & name of pvc %q to get pvc from informer: %v", key, err)
		return nil
	}
	return nil
}
