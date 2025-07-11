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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type CacheDirType string

var (
	CacheDirTypeHostPath             CacheDirType = "HostPath"
	CacheDirTypePVC                  CacheDirType = "PVC"
	CacheDirTypeVolumeClaimTemplates CacheDirType = "VolumeClaimTemplates"
)

type CacheDir struct {
	// +kubebuilder:validation:Enum=HostPath;PVC;VolumeClaimTemplates
	Type CacheDirType `json:"type,omitempty"`
	// required for HostPath type
	// +optional
	Path string `json:"path,omitempty"`
	// required for PVC type
	// +optional
	Name string `json:"name,omitempty"`
	// required for VolumeClaimTemplates type
	// +optional
	VolumeClaimTemplate *corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// CacheGroupWorkerTemplate defines cache group worker template
type CacheGroupWorkerTemplate struct {
	Labels             map[string]string   `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`
	Annotations        map[string]string   `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
	NodeSelector       map[string]string   `json:"nodeSelector,omitempty"`
	ServiceAccountName string              `json:"serviceAccountName,omitempty"`
	HostNetwork        *bool               `json:"hostNetwork,omitempty"`
	SchedulerName      string              `json:"schedulerName,omitempty"`
	Tolerations        []corev1.Toleration `json:"tolerations,omitempty"`
	CacheDirs          []CacheDir          `json:"cacheDirs,omitempty"`
	// Container image.
	// More info: https://kubernetes.io/docs/concepts/containers/images
	// This field is optional to allow higher level config management to default or override
	// container images in workload controllers like Deployments and StatefulSets.
	Image string `json:"image,omitempty"`
	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec.
	// If specified, these secrets will be passed to individual puller implementations for them to use.
	// More info: https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Image pull policy.
	// One of Always, Never, IfNotPresent.
	// Defaults to Always if :latest tag is specified, or IfNotPresent otherwise.
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// List of environment variables to set in the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Compute Resources required by this container.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Periodic probe of container liveness.
	// Container will be restarted if the probe fails.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// Periodic probe of container service readiness.
	// Container will be removed from service endpoints if the probe fails.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// StartupProbe indicates that the Pod has successfully initialized.
	// If specified, no other probes are executed until this completes successfully.
	// If this probe fails, the Pod will be restarted, just as if the livenessProbe failed.
	// This can be used to provide different probe parameters at the beginning of a Pod's lifecycle,
	// when it might take a long time to load data or warm a cache, than during steady-state operation.
	// This cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
	// SecurityContext defines the security options the container should be run with.
	// If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Actions that the management system should take in response to container lifecycle events.
	// +optional
	Lifecycle *corev1.Lifecycle `json:"lifecycle,omitempty"`
	// Pod volumes to mount into the container's filesystem.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// volumeDevices is the list of block devices to be used by the container.
	// +optional
	VolumeDevices []corev1.VolumeDevice `json:"volumeDevices,omitempty"`
	// List of volumes that can be mounted by containers belonging to the pod.
	// More info: https://kubernetes.io/docs/concepts/storage/volumes
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// Set DNS policy for the pod.
	// Defaults to "ClusterFirst".
	// Valid values are 'ClusterFirstWithHostNet', 'ClusterFirst', 'Default' or 'None'.
	// DNS parameters given in DNSConfig will be merged with the policy selected with DNSPolicy.
	// To have DNS options set along with hostNetwork, you have to specify DNS policy
	// explicitly to 'ClusterFirstWithHostNet'.
	// +optional
	DNSPolicy *corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	Opts []string `json:"opts,omitempty"`
}

type CacheGroupWorkerOverwrite struct {
	// select nodes to overwrite
	Nodes                    []string `json:"nodes,omitempty"`
	CacheGroupWorkerTemplate `json:",inline"`
}

type CacheGroupWorkerSpec struct {
	// +kubebuilder:validation:Required
	Template  CacheGroupWorkerTemplate    `json:"template,omitempty"`
	Overwrite []CacheGroupWorkerOverwrite `json:"overwrite,omitempty"`
}

// CacheGroupSpec defines the desired state of CacheGroup
type CacheGroupSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	UpdateStrategy *appsv1.DaemonSetUpdateStrategy `json:"updateStrategy,omitempty"`
	SecretRef      *corev1.SecretEnvSource         `json:"secretRef,omitempty"`
	CleanCache     bool                            `json:"cleanCache,omitempty"`
	CacheGroup     string                          `json:"cacheGroup,omitempty"`
	// Replicas is the number of desired Pods.
	// +kubebuilder:validation:Optional
	Replicas *int32               `json:"replicas,omitempty"`
	Worker   CacheGroupWorkerSpec `json:"worker,omitempty"`
	// Duration for new node to join cluster with group-backup option
	// Default is 10 minutes
	// +optional
	BackupDuration *metav1.Duration `json:"backupDuration,omitempty"`
	// Maximum time to wait for data migration when deleting
	// Default is 1 hour
	// +optional
	WaitingDeletedMaxDuration *metav1.Duration `json:"waitingDeletedMaxDuration,omitempty"`
}

type CacheGroupPhase string

const (
	CacheGroupPhaseWaiting     CacheGroupPhase = "Waiting"
	CacheGroupPhaseProgressing CacheGroupPhase = "Progressing"
	CacheGroupPhaseReady       CacheGroupPhase = "Ready"
)

// CacheGroupCondition defines the observed state of CacheGroup
type CacheGroupCondition struct {
	Type               string      `json:"type"`
	Status             string      `json:"status"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
}

// CacheGroupStatus defines the observed state of CacheGroup
type CacheGroupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase      CacheGroupPhase       `json:"phase,omitempty"`
	Conditions []CacheGroupCondition `json:"conditions,omitempty"`

	FileSystem           string `json:"fileSystem,omitempty"`
	ReadyWorker          int32  `json:"readyWorker,omitempty"`
	BackUpWorker         int32  `json:"backUpWorker,omitempty"`
	WaitingDeletedWorker int32  `json:"waitingDeletedWorker,omitempty"`
	ExpectWorker         int32  `json:"expectWorker,omitempty"`
	ReadyStr             string `json:"readyStr,omitempty"`
	CacheGroup           string `json:"cacheGroup,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cg
// +kubebuilder:printcolumn:name="Cache Group",type="string",JSONPath=".status.cacheGroup"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Back up",type="string",JSONPath=".status.backUpWorker"
// +kubebuilder:printcolumn:name="Waiting Deleted",type="string",JSONPath=".status.WaitingDeletedWorker"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyStr"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// CacheGroup is the Schema for the cachegroups API
type CacheGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CacheGroupSpec   `json:"spec,omitempty"`
	Status CacheGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CacheGroupList contains a list of CacheGroup
type CacheGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CacheGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CacheGroup{}, &CacheGroupList{})
}
