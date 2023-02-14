/*
Copyright 2022.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VolumeSnapshotRollBackSpec defines the desired state of VolumeSnapshotRollBack
type VolumeSnapshotRollBackSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	VolumeSnapshot            string `json:"volumeSnapshot,omitempty"`
	PersistentVolumeClaimName string `json:"persistentVolumeClaimName,omitempty"`
	Force                     bool   `json:"force,omitempty"`
}

// VolumeSnapshotRollBackStatus defines the observed state of VolumeSnapshotRollBack
type VolumeSnapshotRollBackStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Process string `json:"process,omitempty"`
	Mesage  string `json:"message,omitempty"`
}

//+kubebuilder:printcolumn:JSONPath=".status.process",name=process,type=string
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VolumeSnapshotRollBack is the Schema for the volumesnapshotrollbacks API
type VolumeSnapshotRollBack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeSnapshotRollBackSpec   `json:"spec,omitempty"`
	Status VolumeSnapshotRollBackStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VolumeSnapshotRollBackList contains a list of VolumeSnapshotRollBack
type VolumeSnapshotRollBackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeSnapshotRollBack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VolumeSnapshotRollBack{}, &VolumeSnapshotRollBackList{})
}
