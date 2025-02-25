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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ParsedSyncSink struct {
	Uri  string
	Envs []corev1.EnvVar
	Auth string
}

type SyncSinkValue struct {
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *corev1.EnvVarSource `json:"valueFrom,omitempty" protobuf:"bytes,3,opt,name=valueFrom"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type SyncSinkExternal struct {
	// +kubebuilder:validation:Required
	Uri string `json:"uri"`

	AccessKey SyncSinkValue `json:"accessKey,omitempty"`
	SecretKey SyncSinkValue `json:"secretKey,omitempty"`
}

type SyncSinkJuiceFS struct {
	// +kubebuilder:validation:Required
	VolumeName string `json:"volumeName"`
	// +kubebuilder:validation:Required
	Token SyncSinkValue `json:"token"`

	// +optional
	Path string `json:"path"`

	AccessKey   SyncSinkValue `json:"accessKey,omitempty"`
	SecretKey   SyncSinkValue `json:"secretKey,omitempty"`
	AuthOptions []string      `json:"authOptions,omitempty"`

	// Required in on-premise environment
	ConsoleUrl string `json:"consoleUrl,omitempty"`
}

type SyncSinkPVC struct {
	// +kubebuilder:validation:Required
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace,omitempty"`
}

type SyncSink struct {
	// Sync from external source
	External *SyncSinkExternal `json:"external,omitempty"`

	// Sync from JuiceFS
	JuiceFS *SyncSinkJuiceFS `json:"juicefs,omitempty"`

	// Sync from PVC
	PVC *SyncSinkPVC `json:"pvc,omitempty"`
}

// SyncSpec defines the desired state of Sync.
type SyncSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Image of sync worker.
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Number of worker.
	// zero and not specified. Defaults to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Node selector
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`

	// Resources
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Sync Options
	// ref: https://juicefs.com/docs/cloud/reference/command_reference/#sync
	Options []string `json:"options,omitempty"`

	// +kubebuilder:validation:Required
	From SyncSink `json:"from,omitempty"`
	// +kubebuilder:validation:Required
	To SyncSink `json:"to,omitempty"`
}

type SyncPhase string

const (
	SyncPhaseWaiting     SyncPhase = "Waiting"
	SyncPhasePreparing   SyncPhase = "Preparing"
	SyncPhaseProgressing SyncPhase = "Progressing"
	SyncPhaseFailed      SyncPhase = "Failed"
	SyncPhaseCompleted   SyncPhase = "Completed"
)

// SyncStatus defines the observed state of Sync.
type SyncStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:default=Waiting
	Phase SyncPhase `json:"phase,omitempty"`

	// +optional
	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Sync is the Schema for the syncs API.
type Sync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SyncSpec   `json:"spec,omitempty"`
	Status SyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SyncList contains a list of Sync.
type SyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Sync{}, &SyncList{})
}
