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

// WebDAVSpec defines the desired state of WebDAV.
type WebDAVSpec struct {
	// Image of the WebDAV server.
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling images.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ImagePullPolicy is the image pull policy.
	// One of Always, Never, IfNotPresent.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Replicas is the number of WebDAV server replicas.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// SecretRef references the secret containing JuiceFS credentials.
	// +kubebuilder:validation:Required
	SecretRef *corev1.SecretEnvSource `json:"secretRef"`

	// Port is the WebDAV server listening port.
	// +kubebuilder:default=9007
	// +optional
	Port int32 `json:"port,omitempty"`

	// ServiceType is the Kubernetes Service type to expose the WebDAV server.
	// +kubebuilder:default=ClusterIP
	// +optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// Options are additional options passed to the juicefs webdav command.
	// +optional
	Options []string `json:"options,omitempty"`

	// Env is a list of environment variables to set in the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Resources defines the compute resources required by the container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations are the pod's tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity defines scheduling constraints for the pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Labels are extra labels to add to the WebDAV pods.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are extra annotations to add to the WebDAV pods.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

type WebDAVPhase string

const (
	WebDAVPhasePending     WebDAVPhase = "Pending"
	WebDAVPhaseProgressing WebDAVPhase = "Progressing"
	WebDAVPhaseReady       WebDAVPhase = "Ready"
)

// WebDAVStatus defines the observed state of WebDAV.
type WebDAVStatus struct {
	// Phase is the current phase of the WebDAV server.
	// +kubebuilder:default=Pending
	Phase WebDAVPhase `json:"phase,omitempty"`

	// Replicas is the total number of WebDAV pods.
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready WebDAV pods.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=wdav
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// WebDAV is the Schema for the webdavs API.
type WebDAV struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec WebDAVSpec `json:"spec,omitempty"`
	// +kubebuilder:default={phase: Pending}
	Status WebDAVStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WebDAVList contains a list of WebDAV.
type WebDAVList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WebDAV `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WebDAV{}, &WebDAVList{})
}
