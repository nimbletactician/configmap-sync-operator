/*
Copyright 2024.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapSyncSpec defines the desired state of ConfigMapSync
type ConfigMapSyncSpec struct {
	// Source config map is the name of the configmap that we will sync
	SourceConfigMap string `json:"sourceConfigMap"`
	// TargetNameSpaces is the list of namespaces to sync the configMap to
	TargetNameSpaces []string `json:"targetNamespaces"`
	// Labels is an optional map of labels to apply to synced ConfigMaps
	// +optional
	Labels map[string]string `json:"labels,omitempty"` // Note: Changed Label to Labels
}

// ConfigMapSyncStatus defines the observed state of ConfigMapSync
type ConfigMapSyncStatus struct {
	// LastSyncTime is the last time the ConfigMap was successfully synced
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// SyncedNamespace is the list of namespaces where the configmap has been successfully synced
	SyncedNamespaces []string `json:"syncedNamespaces,omitempty"`
	// Conditions represent the latest available observatiomake manifestsns of the configmap synced state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ConfigMapSync is the Schema for the configmapsyncs API
type ConfigMapSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigMapSyncSpec   `json:"spec,omitempty"`
	Status ConfigMapSyncStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ConfigMapSyncList contains a list of ConfigMapSync
type ConfigMapSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMapSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMapSync{}, &ConfigMapSyncList{})
}
