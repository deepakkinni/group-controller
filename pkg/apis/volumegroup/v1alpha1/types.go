/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// +kubebuilder:object:generate=true
package v1alpha1

import (
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeGroup is a user's request for a group of volumes
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=vg
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`,description="Indicates if the VolumeGroup is ready."
// +kubebuilder:printcolumn:name="VolumeGroupClassName",type=string,JSONPath=`.spec.volumeGroupClassName`,description="The name of the VolumeGroupClass."
// +kubebuilder:printcolumn:name="VolumeGroupContentName",type=string,JSONPath=`.spec.volumeGroupContentName`,description="Name of the VolumeGroupContentName object."
// +kubebuilder:printcolumn:name="GroupCreationTime",type=date,JSONPath=`.status.groupCreationTime`,description="Timestamp when the VolumeGroup was created"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type VolumeGroup struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the volume group requested by a user
	Spec VolumeGroupSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information about a volume group
	// +optional
	Status *VolumeGroupStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// VolumeGroupList is a collection of VolumeGroup.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VolumeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// items is the list of VolumeGroup
	Items []VolumeGroup `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// VolumeGroupSpec describes the common attributes of group storage devices
// and allows a Source for provider-specific attributes
type VolumeGroupSpec struct {
	// +optional
	VolumeGroupClassName *string `json:"volumeGroupClassName,omitempty" protobuf:"bytes,1,opt,name=volumeGroupClassName"`

	// VolumeGroupContentName is the binding reference to the VolumeGroupContent
	// backing this VolumeGroup
	// +optional
	VolumeGroupContentName *string `json:"volumeGroupContentName,omitempty" protobuf:"bytes,2,opt,name=volumeGroupContentName"`
}

type VolumeGroupStatus struct {
	// +optional
	GroupCreationTime *metav1.Time `json:"groupCreationTime,omitempty" protobuf:"bytes,1,opt,name=groupCreationTime"`

	// A list of persistent volume claims
	// +optional
	PVCList []core_v1.PersistentVolumeClaim `json:"pvcList" protobuf:"bytes,2,rep,name=pvcList"`

	// +optional
	Ready *bool `json:"ready,omitempty" protobuf:"varint,3,opt,name=ready"`

	// Last error encountered during group creation
	// +optional
	Error *VolumeGroupError `json:"error,omitempty" protobuf:"bytes,4,opt,name=error,casttype=VolumeGroupError"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeGroupClass specifies the class
// VolumeGroupClasses are non-namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=vgclass;vgclasses
// +kubebuilder:printcolumn:name="Driver",type=string,JSONPath=`.driver`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="SupportVolumeGroupSnapshot",type=boolean,JSONPath=`.supportvolumegroupsnapshot`
type VolumeGroupClass struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Driver is the driver expected to handle this VolumeGroupClass.
	// This value may not be empty.
	Driver string `json:"driver" protobuf:"bytes,2,opt,name=driver"`

	// Parameters hold parameters for the driver.
	// These values are opaque to the system and are passed directly
	// to the driver.
	// +optional
	Parameters map[string]string `json:"parameters,omitempty" protobuf:"bytes,3,rep,name=parameters"`

	// This field specifies whether group snapshot is supported.
	// The default is false.
	// +optional
	SupportVolumeGroupSnapshot *bool `json:"supportVolumeGroupSnapshot,omitempty" protobuf:"varint,4,opt,name=supportVolumeGroupSnapshot"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeGroupClassList is a collection of VolumeGroupClasses.
// +kubebuilder:object:root=true
type VolumeGroupClassList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// items is the list of VolumeSnapshotClasses
	Items []VolumeGroupClass `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeGroupContent
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=vgc;vgcs
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`,description="Indicates if the VolumeGroup is ready"
// +kubebuilder:printcolumn:name="VolumeGroupDeletionPolicy",type=string,JSONPath=`.spec.volumeGroupDeletionPolicy`,description="Deletion policy associated with VolumeGroup"
// +kubebuilder:printcolumn:name="Driver",type=string,JSONPath=`.spec.source.driver`,description="Name of the CSI driver."
// +kubebuilder:printcolumn:name="VolumeGroupClassName",type=string,JSONPath=`.spec.volumeGroupClassName`,description="Name of the VolumeGroupClass."
// +kubebuilder:printcolumn:name="VolumeGroup",type=string,JSONPath=`.spec.volumeGroupRef.name`,description="Name of the VolumeGroup object to which this VolumeGroupContent object is bound."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// VolumeGroupContent represents a group of volumes on the storage backend
type VolumeGroupContent struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the volume group requested by a user
	Spec VolumeGroupContentSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information about a volume group
	// +optional
	Status *VolumeGroupContentStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VolumeGroupContentList is a list of VolumeGroupContent objects
// +kubebuilder:object:root=true
type VolumeGroupContentList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// items is the list of VolumeGroupContent
	Items []VolumeGroupContent `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// VolumeGroupContentSpec is the associated spec.
type VolumeGroupContentSpec struct {
	// +optional
	VolumeGroupClassName *string `json:"volumeGroupClassName,omitempty" protobuf:"bytes,1,opt,name=volumeGroupClassName"`

	// +optional
	// VolumeGroupRef is part of a bi-directional binding between VolumeGroup and VolumeGroupContent.
	VolumeGroupRef *core_v1.ObjectReference `json:"volumeGroupRef" protobuf:"bytes,2,opt,name=volumeGroupRef"`

	// +optional
	Source *VolumeGroupContentSource `json:"source" protobuf:"bytes,3,opt,name=source"`

	// +optional
	VolumeGroupDeletionPolicy *VolumeGroupDeletionPolicy `json:"volumeGroupDeletionPolicy" protobuf:"bytes,4,opt,name=volumeGroupDeletionPolicy"`
}

// VolumeGroupContentSource
type VolumeGroupContentSource struct {
	// Required
	Driver string `json:"driver" protobuf:"bytes,1,opt,name=driver"`

	// VolumeGroupHandle is the unique volume group name returned by the
	// CSI volume plugin’s CreateVolumeGroup to refer to the volume group on
	// all subsequent calls.
	// Required.
	VolumeGroupHandle string `json:"volumeGroupHandle" protobuf:"bytes,2,opt,name=volumeGroupHandle"`

	// Attributes of the volume group to publish.
	// +optional
	VolumeGroupAttributes map[string]string `json:"volumeGroupAttributes,omitempty" protobuf:"bytes,3,rep,name=volumeGroupAttributes"`
}

type VolumeGroupContentStatus struct {
	// +optional
	GroupCreationTime *metav1.Time `json:"groupCreationTime,omitempty" protobuf:"bytes,1,opt,name=groupCreationTime"`

	// A list of persistent volumes
	// +optional
	PVList []core_v1.PersistentVolume `json:"pvList" protobuf:"bytes,2,rep,name=pvList"`

	// +optional
	Ready *bool `json:"ready,omitempty" protobuf:"varint,3,opt,name=ready"`

	// Last error encountered during group creation
	// +optional
	Error *VolumeGroupError `json:"error,omitempty" protobuf:"bytes,4,opt,name=error,casttype=VolumeGroupError"`
}

// VolumeGroupDeletionPolicy
// +kubebuilder:validation:Enum=Delete;Retain
type VolumeGroupDeletionPolicy string

const (
	// volumeGroupContentRetain
	VolumeGroupContentDelete VolumeGroupDeletionPolicy = "Delete"

	// volumeGroupContentRetain
	VolumeGroupContentRetain VolumeGroupDeletionPolicy = "Retain"
)

// VolumeGroupError describes an error
type VolumeGroupError struct {
	// time is the timestamp when the error was encountered.
	// +optional
	Time *metav1.Time `json:"time,omitempty" protobuf:"bytes,1,opt,name=time"`

	// message is a string detailing the encountered error during snapshot
	// creation if specified.
	// NOTE: message may be logged, and it should not contain sensitive
	// information.
	// +optional
	Message *string `json:"message,omitempty" protobuf:"bytes,2,opt,name=message"`
}
