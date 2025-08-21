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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type WarmUpTargetsFrom struct {
	Files     []string                     `json:"files,omitempty"`
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`
	FilePath  string                       `json:"filePath,omitempty"`
}

type WarmUpStats struct {
	// +kubebuilder:default=0
	Scanned int64 `json:"scanned,omitempty"`
	// +kubebuilder:default=0
	Completed int64 `json:"completed,omitempty"`
	// +kubebuilder:default=0
	FailedBlocks int64 `json:"failedBlocks,omitempty"`

	Speed         string `json:"speed,omitempty"`
	ScannedData   string `json:"scannedData,omitempty"`
	CompletedData string `json:"completedData,omitempty"`
	FailedData    string `json:"failedData,omitempty"`
}

// WarmUpSpec defines the desired state of WarmUp
type WarmUpSpec struct {
	CacheGroupName          string                      `json:"cacheGroupName"`
	BackoffLimit            *int32                      `json:"backoffLimit,omitempty"`
	TtlSecondsAfterFinished *int32                      `json:"ttlSecondsAfterFinished,omitempty"`
	Metadata                Metadata                    `json:"metadata,omitempty"`
	Tolerations             []corev1.Toleration         `json:"tolerations,omitempty"`
	NodeSelector            map[string]string           `json:"nodeSelector,omitempty"`
	Targets                 []string                    `json:"targets,omitempty"` // @deprecated
	TargetsFrom             *WarmUpTargetsFrom          `json:"targetsFrom,omitempty"`
	Options                 []string                    `json:"options,omitempty"`
	Policy                  Policy                      `json:"policy,omitempty"`
	Resources               corev1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	// Image to use for the warmup job
	// defaults to cachegroup pod image
	Image string `json:"image,omitempty"`
}

type Metadata struct {
	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

type Policy struct {
	Type PolicyType `json:"type,omitempty"`
	Cron Cron       `json:"cron,omitempty"`
}

type PolicyType string

const (
	PolicyTypeOnce PolicyType = "Once"
	PolicyTypeCron PolicyType = "Cron"
)

type Cron struct {
	Schedule string `json:"schedule,omitempty"`
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// WarmUpStatus defines the observed state of WarmUp
type WarmUpStatus struct {
	Phase      WarmUpPhase `json:"phase"`
	CacheGroup string      `json:"cacheGroup,omitempty"`
	Duration   string      `json:"duration,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`

	Stats     WarmUpStats `json:"stats,omitempty"`
	Progress  string      `json:"progress,omitempty"`
	FinishLog string      `json:"finishLog,omitempty"`

	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	LastCompleteTime *metav1.Time `json:"lastCompleteTime,omitempty"`
	LastCompleteNode string       `json:"LastCompleteNode,omitempty"`
}

type WarmUpPhase string

const (
	WarmUpPhasePending  WarmUpPhase = "Pending"
	WarmUpPhaseRunning  WarmUpPhase = "Running"
	WarmUpPhaseFailed   WarmUpPhase = "Failed"
	WarmUpPhaseComplete WarmUpPhase = "Complete"
)

type Condition struct {
	Type               string      `json:"type,omitempty"`
	Status             string      `json:"status,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=wu
// +kubebuilder:printcolumn:name="CacheGroup",type="string",JSONPath=`.spec.cacheGroupName`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Scanned",type="string",JSONPath=".status.stats.scannedData"
// +kubebuilder:printcolumn:name="Completed",type="string",JSONPath=".status.stats.completedData"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="Duration",type="string",JSONPath=`.status.duration`

// WarmUp is the Schema for the warmups API
type WarmUp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WarmUpSpec   `json:"spec,omitempty"`
	Status WarmUpStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WarmUpList contains a list of WarmUp
type WarmUpList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WarmUp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WarmUp{}, &WarmUpList{})
}
