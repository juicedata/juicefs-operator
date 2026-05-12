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

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	// Image of the gateway container.
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ImagePullPolicy defines the image pull policy.
	// One of Always, Never, IfNotPresent.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Replicas is the number of desired gateway Pods.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// SecretRef is a reference to the secret containing JuiceFS EE credentials (name, token, etc.).
	// Required when using JuiceFS Enterprise Edition.
	// +optional
	SecretRef *corev1.SecretEnvSource `json:"secretRef,omitempty"`

	// MetaURL is the metadata engine URL for JuiceFS Community Edition (e.g. redis://...).
	// Required when using JuiceFS Community Edition.
	// +optional
	MetaURL string `json:"metaURL,omitempty"`

	// Address is the listening address for the gateway.
	// +kubebuilder:default="0.0.0.0:9000"
	// +optional
	Address string `json:"address,omitempty"`

	// Options are extra options passed to the `juicefs gateway` command.
	// +optional
	Options []string `json:"options,omitempty"`

	// Env is a list of environment variables to set in the gateway container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Resources defines the compute resources for the gateway container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector constrains scheduling to nodes matching these labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for the gateway pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity for the gateway pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Labels to add to the gateway pods.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations to add to the gateway pods.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// ServiceType is the type of Kubernetes Service to create for the gateway.
	// +kubebuilder:default=ClusterIP
	// +optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// ServiceAnnotations are additional annotations to set on the gateway Service.
	// +optional
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`
}

type GatewayPhase string

const (
	GatewayPhaseProgressing GatewayPhase = "Progressing"
	GatewayPhaseReady       GatewayPhase = "Ready"
)

// GatewayStatus defines the observed state of Gateway
type GatewayStatus struct {
	// Phase is the current phase of the gateway.
	Phase GatewayPhase `json:"phase,omitempty"`

	// ReadyReplicas is the number of gateway pods that are ready.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Replicas is the total number of gateway pods.
	Replicas int32 `json:"replicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gw
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Gateway is the Schema for the gateways API
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewaySpec   `json:"spec,omitempty"`
	Status GatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}
