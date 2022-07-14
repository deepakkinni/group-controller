package volumegroup_sidecar_controller

import (
	"context"
	"fmt"
	"github.com/deepakkinni/volumegroup-controller/pkg/apis/volumegroup/v1alpha1"
	"github.com/deepakkinni/volumegroup-controller/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"strings"
	"time"
)

func (ctrl *csiVolumeGroupSideCarController) syncPersistentVolumeClaim(pvc *v1.PersistentVolumeClaim) error {
	volGroupAnn := pvc.Annotations["storage.k8s.io/volumegroup"]
	if pvc.Status.Phase != v1.ClaimBound {
		klog.Infof("The pvc %s/%s was not in bound phase, ignoring for now", pvc.Namespace, pvc.Name)
		return nil
	}
	if pvc.ObjectMeta.DeletionTimestamp != nil {
		klog.Infof("the pvc %s/%s is under deletion, skipping", pvc.Namespace, pvc.Name)
		return nil
	}
	if volGroupAnn != "" {
		klog.Info("found storage.k8s.io/volumegroup annotation on pvc")
		split := strings.Split(volGroupAnn, "/")
		volumeGroupNamespace := split[0]
		volumeGroupName := split[1]
		// Retrieve the volume group and add the pvc
		volumeGroupObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroupNamespace).Get(context.TODO(), volumeGroupName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error get volumegroup %s from api server: %v", volGroupAnn, err)
		}
		pvcList := volumeGroupObj.Status.PVCList
		found := false
		for _, vgPvc := range pvcList {
			if vgPvc.Namespace == pvc.Namespace && vgPvc.Name == pvc.Name {
				klog.Infof("pvc %s/%s already found on volumegroup", vgPvc.Namespace, vgPvc.Name, volGroupAnn)
				found = true
			} else {
				klog.Infof("existing pvc on volumegroup %s, pvc %s/%s", volGroupAnn,
					vgPvc.Namespace, vgPvc.Name)
			}
		}
		if found {
			klog.Infof("The pvc %s/%s is already present in the volumegroup %s", pvc.Namespace, pvc.Name, volGroupAnn)
			return nil
		}
		pvcList = append(pvcList, *pvc)
		// update the volume group status
		volumeGroupClone := volumeGroupObj.DeepCopy()
		volumeGroupClone.Status.PVCList = pvcList
		updatedVg, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroupClone.Namespace).UpdateStatus(context.TODO(), volumeGroupClone, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		klog.Infof("Successfully updated the volumegroup %s/%s with pvc %s", volumeGroupNamespace, volumeGroupName, pvc.Name)
		_, err = ctrl.storeVolumeGroupUpdate(updatedVg)
		if err != nil {
			klog.Warningf("failed to store the updated volumegroup %s update. err: %+v", utils.VolumeGroupKey(updatedVg), err)
		}
	} else {
		klog.Infof("storage.k8s.io/volumegroup annotation not found on pvc %s/%s", pvc.Namespace, pvc.Name)
	}
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) syncVolumeGroupContent(volumeGroupContent *v1alpha1.VolumeGroupContent) error {
	klog.V(5).Infof("synchronizing VolumeGroupContent[%s]", volumeGroupContent.Name)

	if volumeGroupContent.ObjectMeta.DeletionTimestamp != nil {
		return ctrl.processVolumeGroupContentWithDeletionTimestamp(volumeGroupContent)
	}

	if volumeGroupContent.Status == nil {
		boundVolumeGroupContent := volumeGroupContent.Name
		// trigger a CSI CreateVolumeGroup call and retrieve the volumegroupid
		// TODO: remove the mock
		volumeGroupContentObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Get(context.TODO(), boundVolumeGroupContent, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error get volumegroupcontent %s from api server: %v", boundVolumeGroupContent, err)
		}
		ready := true
		creationTime := metav1.Time{ Time: time.Now()}
		volumeGroupContentStatus := &v1alpha1.VolumeGroupContentStatus {
			GroupCreationTime: &creationTime,
			PVList:            nil,
			Ready:             &ready,
			Error:             nil,
		}
		volumeGroupContentClone := volumeGroupContentObj.DeepCopy()
		volumeGroupContentClone.Status = volumeGroupContentStatus
		updatedVolGroupContent, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().UpdateStatus(context.TODO(), volumeGroupContentClone, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("error update volumegroupcontent status %s from api server: %v", boundVolumeGroupContent, err)
		}
		_ , err = ctrl.storeVolumeGroupClassUpdate(updatedVolGroupContent)
		if err != nil {
			klog.Warningf("failed to store the updated volumegroupcontent %s, err: %+v", updatedVolGroupContent.Name, err)
		}
		// explicitly sync the volumegroup
		volumeGroupNamespace:= volumeGroupContentObj.Spec.VolumeGroupRef.Namespace
		volumeGroupName:= volumeGroupContentObj.Spec.VolumeGroupRef.Name
		ctrl.volumeGroupQueue.Add(volumeGroupNamespace + "/" + volumeGroupName)
		klog.Infof("Queued %s/%s for sync", volumeGroupNamespace, volumeGroupName)
	}
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) syncVolumeGroup(volumeGroup *v1alpha1.VolumeGroup) error {
	klog.V(5).Infof("synchronizing VolumeGroup[%s]: %s", utils.VolumeGroupKey(volumeGroup),
		utils.GetVolumeGroupStatusForLogging(volumeGroup))

	if volumeGroup.ObjectMeta.DeletionTimestamp != nil {
		return ctrl.processVolumeGroupWithDeletionTimestamp(volumeGroup)
	}

	if !utils.IsVolumeGroupReady(volumeGroup) || !utils.IsBoundVolumeGroupContentNameSet(volumeGroup) {
		return ctrl.syncUnreadyVolumeGroup(volumeGroup)
	}
	return ctrl.syncReadyVolumeGroup(volumeGroup)
}

func (ctrl *csiVolumeGroupSideCarController) syncUnreadyVolumeGroup(volumeGroup *v1alpha1.VolumeGroup) error {
	uniqueVolumeGroupName := utils.VolumeGroupKey(volumeGroup)
	klog.V(5).Infof("syncUnreadyVolumeGroup %s", uniqueVolumeGroupName)
	var volumeGroupContent *v1alpha1.VolumeGroupContent
	volumeGroupContent, err := ctrl.createVolumeGroupContent(volumeGroup)
	if err != nil {
		return err
	}
	klog.Infof("successfully created volumegroupcontent: %s", volumeGroupContent.Name)
	// Update the VolumeGroupSpec to the bound content name
	volumeGroupObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroup.Namespace).Get(context.TODO(), volumeGroup.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error get volumegroup %s from api server: %v", utils.VolumeGroupKey(volumeGroup), err)
	}
	if volumeGroupObj.Spec.VolumeGroupContentName == nil || *volumeGroupObj.Spec.VolumeGroupContentName == "" {
		// Update here
		className := "test-volume-group-class"
		volumeGroupObj.Spec.VolumeGroupContentName = &volumeGroupContent.Name
		volumeGroupObj.Spec.VolumeGroupClassName = &className
		klog.Infof("updating volumegroup: %s after creating the volumegroupcontent", volumeGroup.Name)
		volumeGroupNewObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroup.Namespace).Update(context.TODO(),volumeGroupObj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("error updating bound contentname for volumegroup %s in api server: %v", utils.VolumeGroupKey(volumeGroup), err)
		}
		volumeGroupObj = volumeGroupNewObj
	}
	volumeGroupContentObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Get(context.TODO(), volumeGroupContent.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error get volumegroupcontent %s from api server: %v", volumeGroupContent.Name, err)
	}
	klog.V(5).Infof("syncUnreadyVolumeGroup [%s]: trying to update volumegroup status", utils.VolumeGroupKey(volumeGroup))
	if _, err = ctrl.updateVolumeGroupStatus(volumeGroupObj, volumeGroupContentObj); err != nil {
		// update volumegroup status failed
		return err
	}
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) updateVolumeGroupStatus(volumeGroup *v1alpha1.VolumeGroup,
	volumeGroupContent *v1alpha1.VolumeGroupContent) (*v1alpha1.VolumeGroup, error) {
	klog.V(5).Infof("updateVolumeGroupStatus[%s]", utils.VolumeGroupKey(volumeGroup))
	var groupCreationTime *metav1.Time
	if volumeGroupContent.Status != nil && volumeGroupContent.Status.GroupCreationTime != nil {
		groupCreationTime = volumeGroupContent.Status.GroupCreationTime
	}
	var ready *bool
	if volumeGroupContent.Status != nil && volumeGroupContent.Status.Ready != nil {
		klog.Infof("volumegroupcontent status is %t", *volumeGroupContent.Status.Ready)
		ready = volumeGroupContent.Status.Ready
	} else {
		klog.Infof("volumegroupcontent status is unset or Ready is unset")
	}

	var volumeGroupError *v1alpha1.VolumeGroupError
	if volumeGroupContent.Status != nil && volumeGroupContent.Status.Error != nil {
		volumeGroupError = volumeGroupContent.Status.Error
	}
	klog.V(5).Infof("updateVolumeGroupStatus: updating VolumeGroup [%+v] based on VolumeGroupContentStatus [%+v]", volumeGroup, volumeGroupContent.Status)
	volumeGroupObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroup.Namespace).Get(context.TODO(), volumeGroup.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error get volumegroup %s from api server: %v", utils.VolumeGroupKey(volumeGroup), err)
	}
	var newStatus *v1alpha1.VolumeGroupStatus
	updated := false
	if volumeGroupObj.Status == nil {
		newStatus = &v1alpha1.VolumeGroupStatus {
			GroupCreationTime: groupCreationTime,
			PVCList:           nil,
			Ready:             ready,
			Error:             volumeGroupError,
		}
		updated = true
	} else {
		newStatus = volumeGroupObj.Status.DeepCopy()
		if newStatus.GroupCreationTime == nil && groupCreationTime != nil {
			newStatus.GroupCreationTime = groupCreationTime
			updated = true
		}
		if newStatus.Ready == nil && ready != nil {
			newStatus.Ready = ready
			if *ready && newStatus.Error != nil {
				newStatus.Error = nil
			}
			updated = true
		}
	}
	if updated {
		volumeGroupClone := volumeGroupObj.DeepCopy()
		volumeGroupClone.Status = newStatus
		newVolGroupObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroupClone.Namespace).UpdateStatus(context.TODO(), volumeGroupClone, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}
		_, err = ctrl.storeVolumeGroupUpdate(newVolGroupObj)
		if err != nil {
			klog.Warningf("failed to store the updated volumegroup %s update. err: %+v", utils.VolumeGroupKey(newVolGroupObj), err)
		}
		return newVolGroupObj, nil
	}
	return volumeGroupObj, nil
}


func (ctrl *csiVolumeGroupSideCarController) syncReadyVolumeGroup(volumeGroup *v1alpha1.VolumeGroup) error {
	// Here the vg and vgc are bound.
	// the pvc list from vg most likely is not translated into pv list in vgc
	// Use this method to to do the translation and update the vgc.
	volumeGroupObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroups(volumeGroup.Namespace).Get(context.TODO(), volumeGroup.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error get volumegroup %s from api server: %v", utils.VolumeGroupKey(volumeGroup), err)
	}
	if volumeGroupObj.Spec.VolumeGroupContentName != nil && *volumeGroup.Spec.VolumeGroupContentName != "" {
		boundVolumeGroupContent := *volumeGroup.Spec.VolumeGroupContentName
		var pvList []v1.PersistentVolume
		if volumeGroupObj.Status != nil {
			pvcList := volumeGroupObj.Status.PVCList
			for _, pvc := range pvcList {
				if pvc.Spec.VolumeName != "" {
					pvName := pvc.Spec.VolumeName
					pv, err := ctrl.client.CoreV1().PersistentVolumes().Get(context.TODO(), pvName, metav1.GetOptions{})
					if err != nil {
						klog.Errorf("failed to get pv: %s from api server", pvName)
						continue
					}
					pvList = append(pvList, *pv)
				}
			}
		}
		volumeGroupContentObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Get(context.TODO(), boundVolumeGroupContent, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error get volumegroupcontent %s from api server: %v", boundVolumeGroupContent, err)
		}
		volumeGroupContentClone := volumeGroupContentObj.DeepCopy()
		volumeGroupContentStatusObj := volumeGroupContentObj.Status.DeepCopy()
		volumeGroupContentStatusObjPvList := volumeGroupContentObj.Status.PVList
		var pvMap = make(map[string]string)
		for _, pv := range volumeGroupContentStatusObjPvList {
			pvMap[pv.Name] = ""
		}
		newPvFound := false
		var diffPvList []v1.PersistentVolume
		for _, pv := range pvList {
			_, ok := pvMap[pv.Name]
			if !ok {
				diffPvList = append(diffPvList, pv)
				newPvFound = true
			}
		}
		if !newPvFound {
			klog.Infof("No new PVs need to be updated on the volumegroupcontent %s, not updating status", boundVolumeGroupContent)
			return nil
		}
		pvList = append(volumeGroupContentStatusObjPvList, diffPvList...)
		volumeGroupContentStatusObj.PVList = pvList
		volumeGroupContentClone.Status = volumeGroupContentStatusObj
		// Update the volumegroupcontent CR
		updatedVolGroupContent, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().UpdateStatus(context.TODO(), volumeGroupContentClone, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("error update volumegroupcontent status %s from api server: %v", boundVolumeGroupContent, err)
		}
		_ , err = ctrl.storeVolumeGroupClassUpdate(updatedVolGroupContent)
		if err != nil {
			klog.Warningf("failed to store the updated volumegroupcontent %s, err: %+v", updatedVolGroupContent.Name, err)
		}
	}
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) createVolumeGroupContent(volumeGroup *v1alpha1.VolumeGroup) (*v1alpha1.VolumeGroupContent, error) {
	klog.Infof("createVolumeGroupContent: Creating content for volumeGroup %s", utils.VolumeGroupKey(volumeGroup))
	// Create VolumeSnapshotContent name
	volmeGroupContentName := utils.GetDynamicVolumeGroupContentNameForVolumeGroup(volumeGroup)
	volumeGroupRef, err := reference.GetReference(scheme.Scheme, volumeGroup)
	if err != nil {
		return nil, err
	}
	// TODO: Following are hardcoded, change them
	volumeGroupDeletionPol := v1alpha1.VolumeGroupContentDelete
	className := "test-volume-group-class"
	finalizer := []string{"vgc-finalizer"}
	volumeGroupContent := &v1alpha1.VolumeGroupContent{
		ObjectMeta: metav1.ObjectMeta{
			Name:       volmeGroupContentName,
			Finalizers: finalizer,
		},
		Spec:       v1alpha1.VolumeGroupContentSpec{
			VolumeGroupClassName:      &className,
			VolumeGroupRef:            volumeGroupRef,
			Source:                    nil,
			VolumeGroupDeletionPolicy: &volumeGroupDeletionPol,
		},
		Status:     nil,
	}
	var updatedVolumeGroupContent *v1alpha1.VolumeGroupContent
	klog.V(5).Infof("volume group content %+v", volumeGroupContent)
	klog.V(5).Infof("createVolumeGroupContent [%s]: trying to create volume group content %s", utils.VolumeGroupKey(volumeGroup), volumeGroupContent.Name)
	if updatedVolumeGroupContent, err = ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Create(context.TODO(), volumeGroupContent, metav1.CreateOptions{}); err == nil || errors.IsAlreadyExists(err) {
		// Save succeeded.
		if err != nil {
			klog.V(3).Infof("volume group content %q for volumegroup %q already exists, reusing", volumeGroupContent.Name, utils.VolumeGroupKey(volumeGroup))
			err = nil
			vgContent, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Get(context.TODO(), volmeGroupContentName, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("failed to retrieve the existing volumegroupcontent %s", volmeGroupContentName)
				return nil, err
			}
			return vgContent, nil
		} else {
			klog.V(3).Infof("volume group content %q for volumegroup %q created, %v", volumeGroupContent.Name, utils.VolumeGroupKey(volumeGroup), volumeGroupContent)
		}
	}
	if err != nil {
		strerr := fmt.Sprintf("Error creating volume group content object for volumegroup %s: %v.", utils.VolumeGroupKey(volumeGroup), err)
		klog.Error(strerr)
		return nil, err
	}
	_, err = ctrl.storeVolumeGroupContentUpdate(updatedVolumeGroupContent)
	if err != nil {
		klog.Errorf("failed to update volumegroupcontent store %v", err)
	}
	return updatedVolumeGroupContent, nil
}

func (ctrl *csiVolumeGroupSideCarController) deleteVolumeGroupContent(volumeGroupContent *v1alpha1.VolumeGroupContent) {
	klog.V(4).Infof("volumegroupcontent %q attempting to delete..", volumeGroupContent.Name)
	klog.V(4).Infof("volumegroupcontent %q , will attempt to delete associated pvc", volumeGroupContent.Name)

	deletedPvc := make(map[string]string)
	if volumeGroupContent.Status != nil {
		pvList := volumeGroupContent.Status.PVList
		for _, pv := range pvList {
			pvcRef := pv.Spec.ClaimRef
			if pvcRef != nil {
				pvcName := pvcRef.Name
				pvcNamespace := pvcRef.Namespace
				_, ok := deletedPvc[pvcName]
				if ok {
					klog.Infof("the pvc %s/%s was already deleted, skipping", pvcNamespace, pvcName)
					continue
				}
				err := ctrl.client.CoreV1().PersistentVolumeClaims(pvcNamespace).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
				if err != nil {
					klog.Errorf("error while deleteing pvc %s/%s err: %+v", pvcNamespace, pvcName, err)
				}
				klog.Infof("Deleted PVC %s/%s due to volumegroupcontent delete", pvcNamespace, pvcName)
				deletedPvc[pvcName]=""
			}
		}
	} else {
		klog.Infof("The volumegroupcontent status is nil : %+v", volumeGroupContent)
	}
	// Remove the finalizer
	volumeGroupContentObj, err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Get(context.TODO(), volumeGroupContent.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("error get volumegroupcontent %s from api server: %v", volumeGroupContent.Name, err)
	}
	volumeGroupContentObj.Finalizers = nil
	_, err = ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Update(context.TODO(), volumeGroupContentObj, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("error updating volumegroupcontent %s from api server when removing finalizer: %v", volumeGroupContent.Name, err)
	}
	_ = ctrl.volumeGroupContentStore.Delete(volumeGroupContent)
	klog.V(4).Infof("volumegroupcontent %q attempting to deleted", volumeGroupContent.Name)
}

func (ctrl *csiVolumeGroupSideCarController) deleteVolumeGroup(volumeGroup *v1alpha1.VolumeGroup) {
	klog.V(5).Infof("deleteVolumeGroup VolumeGroup[%s]: %s", utils.VolumeGroupKey(volumeGroup),
		utils.GetVolumeGroupStatusForLogging(volumeGroup))
	volumeGroupContentName := utils.GetDynamicVolumeGroupContentNameForVolumeGroup(volumeGroup)
	klog.Infof("volumegroup %s is associated with volumegroupcontent %s",
		utils.VolumeGroupKey(volumeGroup), volumeGroupContentName)

	_ = ctrl.volumeGroupStore.Delete(volumeGroup)
	klog.V(4).Infof("volumegroup %q deleted", utils.VolumeGroupKey(volumeGroup))

	klog.V(5).Infof("deleteVolumeGroup: set DeletionTimeStamp on volumegroupcontent [%s].", volumeGroupContentName)
	err := ctrl.clientset.VolumegroupV1alpha1().VolumeGroupContents().Delete(context.TODO(), volumeGroupContentName, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("failed to delete VolumeGroupContent %s from API server: %q", volumeGroupContentName, err)
	}
}

func (ctrl *csiVolumeGroupSideCarController) processVolumeGroupWithDeletionTimestamp(volumeGroup *v1alpha1.VolumeGroup) error {
	klog.V(5).Infof("processVolumeGroupWithDeletionTimestamp VolumeGroup[%s]: %s", utils.VolumeGroupKey(volumeGroup),
		utils.GetVolumeGroupStatusForLogging(volumeGroup))
	ctrl.deleteVolumeGroup(volumeGroup)
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) processVolumeGroupContentWithDeletionTimestamp(volumeGroupContent *v1alpha1.VolumeGroupContent) error {
	klog.V(5).Infof("processVolumeGroupContentWithDeletionTimestamp VolumeGroupContent %s", volumeGroupContent.Name)
	ctrl.deleteVolumeGroupContent(volumeGroupContent)
	return nil
}

func (ctrl *csiVolumeGroupSideCarController) getVolumeGroupContentFromStore(volumeGroupContentName string) (*v1alpha1.VolumeGroupContent, error) {
	obj, exist, err := ctrl.volumeGroupContentStore.GetByKey(volumeGroupContentName)
	if err != nil {
		// should never reach here based on implementation at:
		// https://github.com/kubernetes/client-go/blob/master/tools/cache/store.go#L226
		return nil, err
	}
	if !exist {
		// not able to find a matching content
		return nil, nil
	}
	content, ok := obj.(*v1alpha1.VolumeGroupContent)
	if !ok {
		return nil, fmt.Errorf("expected VolumeGroupContent, got %+v", obj)
	}
	return content, nil
}