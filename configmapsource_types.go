// api/v1alpha1/configmapsource_types.go

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapSourceSpec defines the desired state of ConfigMapSource
type ConfigMapSourceSpec struct {
	// SourceType specifies the type of source to fetch the configuration from
	// Valid values are: "Git", "File", "ConfigMap", "Secret"
	// +kubebuilder:validation:Enum=Git;File;ConfigMap;Secret
	// +kubebuilder:validation:Required
	SourceType string `json:"sourceType"`

	// Git source configuration
	// +optional
	Git *GitSource `json:"git,omitempty"`

	// File source configuration
	// +optional
	File *FileSource `json:"file,omitempty"`

	// ConfigMap source configuration
	// +optional
	ConfigMap *ConfigMapSource `json:"configMap,omitempty"`

	// Secret source configuration
	// +optional
	Secret *SecretSource `json:"secret,omitempty"`

	// TargetConfigMap is the name of the ConfigMap to be created/updated
	// +kubebuilder:validation:Required
	TargetConfigMap string `json:"targetConfigMap"`

	// TargetNamespace is the namespace where the target ConfigMap will be created
	// If not specified, the same namespace as the ConfigMapSource will be used
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// RefreshInterval is the interval in seconds to sync the ConfigMap with the source
	// If not specified or set to 0, no automatic refresh will be performed
	// +optional
	// +kubebuilder:validation:Minimum=0
	RefreshInterval *int64 `json:"refreshInterval,omitempty"`
}

// GitSource defines Git repository source configuration
type GitSource struct {
	// Repository URL (HTTPS or SSH)
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Branch, tag, or commit SHA to checkout
	// +kubebuilder:validation:Required
	Revision string `json:"revision"`

	// Path within the repository to the configuration files
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// Authentication reference (Secret name)
	// +optional
	AuthSecretRef *SecretReference `json:"authSecretRef,omitempty"`
}

// FileSource defines file source configuration
type FileSource struct {
	// Path to the file or directory containing the configuration
	// +kubebuilder:validation:Required
	Path string `json:"path"`
}

// ConfigMapSource defines an existing ConfigMap as a source
type ConfigMapSource struct {
	// Name of the source ConfigMap
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the source ConfigMap
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Keys to include from the source ConfigMap (if empty, all keys are included)
	// +optional
	Keys []string `json:"keys,omitempty"`
}

// SecretSource defines an existing Secret as a source
type SecretSource struct {
	// Name of the source Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the source Secret
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Keys to include from the source Secret (if empty, all keys are included)
	// +optional
	Keys []string `json:"keys,omitempty"`
}

// SecretReference contains details of a Secret
type SecretReference struct {
	// Name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Secret
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Key within the Secret containing the authentication information
	// For Git, this could be a SSH key or a username:password for HTTPS
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// ConfigMapSourceStatus defines the observed state of ConfigMapSource
type ConfigMapSourceStatus struct {
	// LastSyncTime is the timestamp of the last successful sync
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// LastSyncHash is a hash of the last successfully synced content
	// Used to determine if the source has changed
	// +optional
	LastSyncHash string `json:"lastSyncHash,omitempty"`

	// Conditions represents the latest available observations of the ConfigMapSource's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source Type",type="string",JSONPath=".spec.sourceType"
// +kubebuilder:printcolumn:name="Target ConfigMap",type="string",JSONPath=".spec.targetConfigMap"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ConfigMapSource is the Schema for the configmapsources API
type ConfigMapSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigMapSourceSpec   `json:"spec,omitempty"`
	Status ConfigMapSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigMapSourceList contains a list of ConfigMapSource
type ConfigMapSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMapSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMapSource{}, &ConfigMapSourceList{})
}
