package utils

import (
	"fmt"
	"github.com/deepakkinni/volumegroup-controller/pkg/apis/volumegroup/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"strconv"
)

var keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

// storeObjectUpdate updates given cache with a new object version from Informer
// callback (i.e. with events from etcd) or with an object modified by the
// controller itself. Returns "true", if the cache was updated, false if the
// object is an old version and should be ignored.
func StoreObjectUpdate(store cache.Store, obj interface{}, className string) (bool, error) {
	objName, err := keyFunc(obj)
	if err != nil {
		return false, fmt.Errorf("Couldn't get key for object %+v: %v", obj, err)
	}
	oldObj, found, err := store.Get(obj)
	if err != nil {
		return false, fmt.Errorf("Error finding %s %q in controller cache: %v", className, objName, err)
	}

	objAccessor, err := meta.Accessor(obj)
	if err != nil {
		return false, err
	}

	if !found {
		// This is a new object
		klog.V(4).Infof("storeObjectUpdate: adding %s %q, version %s", className, objName, objAccessor.GetResourceVersion())
		if err = store.Add(obj); err != nil {
			return false, fmt.Errorf("error adding %s %q to controller cache: %v", className, objName, err)
		}
		return true, nil
	}

	oldObjAccessor, err := meta.Accessor(oldObj)
	if err != nil {
		return false, err
	}

	objResourceVersion, err := strconv.ParseInt(objAccessor.GetResourceVersion(), 10, 64)
	if err != nil {
		return false, fmt.Errorf("error parsing ResourceVersion %q of %s %q: %s", objAccessor.GetResourceVersion(), className, objName, err)
	}
	oldObjResourceVersion, err := strconv.ParseInt(oldObjAccessor.GetResourceVersion(), 10, 64)
	if err != nil {
		return false, fmt.Errorf("error parsing old ResourceVersion %q of %s %q: %s", oldObjAccessor.GetResourceVersion(), className, objName, err)
	}

	// Throw away only older version, let the same version pass - we do want to
	// get periodic sync events.
	if oldObjResourceVersion > objResourceVersion {
		klog.V(4).Infof("storeObjectUpdate: ignoring %s %q version %s", className, objName, objAccessor.GetResourceVersion())
		return false, nil
	}

	klog.V(4).Infof("storeObjectUpdate updating %s %q with version %s", className, objName, objAccessor.GetResourceVersion())
	if err = store.Update(obj); err != nil {
		return false, fmt.Errorf("error updating %s %q in controller cache: %v", className, objName, err)
	}
	return true, nil
}

func VolumeGroupKey(vg *v1alpha1.VolumeGroup) string {
	return fmt.Sprintf("%s/%s", vg.Namespace, vg.Name)
}

func GetVolumeGroupStatusForLogging(vg *v1alpha1.VolumeGroup) string {
	volumeGroupContentName := ""
	if vg.Status != nil && vg.Spec.VolumeGroupContentName != nil {
		volumeGroupContentName = *vg.Spec.VolumeGroupContentName
	}
	ready := false
	if vg.Status != nil && vg.Status.Ready != nil {
		ready = *vg.Status.Ready
	}
	return fmt.Sprintf("bound to: %q, Completed: %v", volumeGroupContentName, ready)
}

func GetDynamicVolumeGroupContentNameForVolumeGroup(vg *v1alpha1.VolumeGroup) string {
	return "volumegroupcontent-" + string(vg.UID)
}

func IsVolumeGroupReady(vg *v1alpha1.VolumeGroup) bool {
	if vg.Status == nil || vg.Status.Ready == nil || *vg.Status.Ready == false {
		return false
	}
	return true
}

func IsBoundVolumeGroupContentNameSet(vg *v1alpha1.VolumeGroup) bool {
	if vg.Status == nil || vg.Spec.VolumeGroupContentName == nil || *vg.Spec.VolumeGroupContentName == "" {
		return false
	}
	return true
}