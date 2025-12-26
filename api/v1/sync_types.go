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

type nameMountPath struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`
}

type HostPath struct {
	// +kubebuilder:validation:Required
	Path string               `json:"path"`
	Type *corev1.HostPathType `json:"type,omitempty"`
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`
}

type PVCMountPath struct {
	// +kubebuilder:validation:Required
	ClaimName string `json:"claimName"`
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`
}

type ExtraVolume struct {
	ConfigMap *nameMountPath `json:"configMap,omitempty"`
	Secret    *nameMountPath `json:"secret,omitempty"`
	HostPath  *HostPath      `json:"hostPath,omitempty"`
	PVC       *PVCMountPath  `json:"pvc,omitempty"`
}

type ParsedSyncSink struct {
	Uri            string
	Envs           []corev1.EnvVar
	PrepareCommand string
	FilesFrom      *SyncFilesFrom `json:"filesFrom,omitempty"`

	ExtraVolumes []ExtraVolume `json:"extraVolumes,omitempty"`
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

	AccessKey SyncSinkValue  `json:"accessKey,omitempty"`
	SecretKey SyncSinkValue  `json:"secretKey,omitempty"`
	FilesFrom *SyncFilesFrom `json:"filesFrom,omitempty"`

	ExtraVolumes []ExtraVolume `json:"extraVolumes,omitempty"`
}

type SyncFilesFrom struct {
	Files     []string                     `json:"files,omitempty"`
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`
	FilePath  string                       `json:"filePath,omitempty"`
}

type SyncSinkJuiceFS struct {
	// +kubebuilder:validation:Required
	VolumeName string `json:"volumeName"`
	// +kubebuilder:validation:Required
	Token SyncSinkValue `json:"token"`

	// +optional
	Path        string         `json:"path,omitempty"`
	AccessKey   SyncSinkValue  `json:"accessKey,omitempty"`
	SecretKey   SyncSinkValue  `json:"secretKey,omitempty"`
	AuthOptions []string       `json:"authOptions,omitempty"`
	FilesFrom   *SyncFilesFrom `json:"filesFrom,omitempty"`

	// +optional
	ExtraVolumes []ExtraVolume `json:"extraVolumes,omitempty"`

	// Required in on-premise environment
	ConsoleUrl string `json:"consoleUrl,omitempty"`
}

type SyncSinkJuiceFSCE struct {
	// +kubebuilder:validation:Required
	MetaURL      string        `json:"metaURL"`
	MetaPassWord SyncSinkValue `json:"metaPassWord,omitempty"`
	// +optional
	Path      string         `json:"path,omitempty"`
	FilesFrom *SyncFilesFrom `json:"filesFrom,omitempty"`

	// +optional
	ExtraVolumes []ExtraVolume `json:"extraVolumes,omitempty"`
}

type SyncSink struct {
	// Sync from external source
	External *SyncSinkExternal `json:"external,omitempty"`

	// Sync from JuiceFS enterprise edition
	JuiceFS *SyncSinkJuiceFS `json:"juicefs,omitempty"`

	// Sync from JuiceFS community edition
	JuiceFSCE *SyncSinkJuiceFSCE `json:"juicefsCE,omitempty"`
}

// SyncSpec defines the desired state of Sync.
type SyncSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Image of sync worker.
	// +kubebuilder:validation:Required
	Image string `json:"image"`
	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of the images used by this PodSpec.
	// If specified, these secrets will be passed to individual puller implementations for them to use.
	// More info: https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Image pull policy.
	// One of Always, Never, IfNotPresent.
	// Defaults to Always if :latest tag is specified, or IfNotPresent otherwise.
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Number of worker.
	// zero and not specified. Defaults to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Node selector
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`

	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Resources is the resources for both manager and worker pods.
	// Deprecated: Use ManagerResources and WorkerResources instead.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ManagerResources is the resources for the sync manager pod.
	// If not specified, Resources will be used. If Resources is also not specified, an empty ResourceRequirements will be used.
	ManagerResources *corev1.ResourceRequirements `json:"managerResources,omitempty"`

	// WorkerResources is the resources for the sync worker pods.
	// If not specified, Resources will be used. If Resources is also not specified, an empty ResourceRequirements will be used.
	WorkerResources *corev1.ResourceRequirements `json:"workerResources,omitempty"`

	// Sync Options
	// ref: https://juicefs.com/docs/cloud/reference/command_reference/#sync
	Options []string `json:"options,omitempty"`

	// +kubebuilder:validation:Required
	From SyncSink `json:"from,omitempty"`
	// +kubebuilder:validation:Required
	To SyncSink `json:"to,omitempty"`

	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// PreparingTimeoutSeconds defines the timeout duration for the Preparing phase.
	// If the Preparing phase exceeds this duration, the sync will be marked as Failed.
	// +optional
	PreparingTimeoutSeconds *int32 `json:"PreparingTimeoutSeconds,omitempty"`
}

type SyncPhase string

const (
	SyncPhasePending     SyncPhase = "Pending"
	SyncPhasePreparing   SyncPhase = "Preparing"
	SyncPhaseProgressing SyncPhase = "Progressing"
	SyncPhaseFailed      SyncPhase = "Failed"
	SyncPhaseCompleted   SyncPhase = "Completed"
)

type SyncStats struct {
	// +kubebuilder:default=0
	Handled int64 `json:"handled,omitempty"`
	// +kubebuilder:default=0
	Copied int64 `json:"copied,omitempty"`
	// +kubebuilder:default=0
	Failed int64 `json:"failed,omitempty"`
	// +kubebuilder:default=0
	Skipped int64 `json:"skipped,omitempty"`
	// +kubebuilder:default=0
	Checked int64 `json:"checked,omitempty"`
	// +kubebuilder:default=0
	Lost int64 `json:"lost,omitempty"`
	// +kubebuilder:default=0
	Scanned int64 `json:"scanned,omitempty"`
	// +kubebuilder:default=0
	Pending int64 `json:"pending,omitempty"`
	// +kubebuilder:default=0
	Deleted int64 `json:"deleted,omitempty"`
	// +kubebuilder:default=0
	Extra int64 `json:"extra,omitempty"`
	// +kubebuilder:default=0
	Excluded int64 `json:"excluded,omitempty"`

	CopiedBytes  int64 `json:"copiedBytes,omitempty"`
	CheckedBytes int64 `json:"checkedBytes,omitempty"`
	SkippedBytes int64 `json:"skippedBytes,omitempty"`
	ExtraBytes   int64 `json:"extraBytes,omitempty"`
	ExcludeBytes int64 `json:"excludeBytes,omitempty"`

	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// SyncStatus defines the observed state of Sync.
type SyncStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:default=Pending
	Phase SyncPhase `json:"phase,omitempty"`

	Progress string    `json:"progress,omitempty"`
	Stats    SyncStats `json:"stats,omitempty"`

	// preparing start at
	PreparingAt *metav1.Time `json:"preparingAt,omitempty"`
	// sync start at
	StartAt *metav1.Time `json:"startAt,omitempty"`
	// sync complete at
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
	// +optional
	Reason    string `json:"reason,omitempty"`
	FinishLog string `json:"finishLog,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// Sync is the Schema for the syncs API.
type Sync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SyncSpec `json:"spec,omitempty"`
	// +kubebuilder:default={phase: Pending}
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
