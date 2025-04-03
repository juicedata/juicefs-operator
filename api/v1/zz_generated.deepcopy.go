//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheDir) DeepCopyInto(out *CacheDir) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheDir.
func (in *CacheDir) DeepCopy() *CacheDir {
	if in == nil {
		return nil
	}
	out := new(CacheDir)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroup) DeepCopyInto(out *CacheGroup) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroup.
func (in *CacheGroup) DeepCopy() *CacheGroup {
	if in == nil {
		return nil
	}
	out := new(CacheGroup)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CacheGroup) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupCondition) DeepCopyInto(out *CacheGroupCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupCondition.
func (in *CacheGroupCondition) DeepCopy() *CacheGroupCondition {
	if in == nil {
		return nil
	}
	out := new(CacheGroupCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupList) DeepCopyInto(out *CacheGroupList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CacheGroup, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupList.
func (in *CacheGroupList) DeepCopy() *CacheGroupList {
	if in == nil {
		return nil
	}
	out := new(CacheGroupList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CacheGroupList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupSpec) DeepCopyInto(out *CacheGroupSpec) {
	*out = *in
	if in.UpdateStrategy != nil {
		in, out := &in.UpdateStrategy, &out.UpdateStrategy
		*out = new(appsv1.DaemonSetUpdateStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.SecretRef != nil {
		in, out := &in.SecretRef, &out.SecretRef
		*out = new(corev1.SecretEnvSource)
		(*in).DeepCopyInto(*out)
	}
	in.Worker.DeepCopyInto(&out.Worker)
	if in.BackupDuration != nil {
		in, out := &in.BackupDuration, &out.BackupDuration
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.WaitingDeletedMaxDuration != nil {
		in, out := &in.WaitingDeletedMaxDuration, &out.WaitingDeletedMaxDuration
		*out = new(metav1.Duration)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupSpec.
func (in *CacheGroupSpec) DeepCopy() *CacheGroupSpec {
	if in == nil {
		return nil
	}
	out := new(CacheGroupSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupStatus) DeepCopyInto(out *CacheGroupStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]CacheGroupCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupStatus.
func (in *CacheGroupStatus) DeepCopy() *CacheGroupStatus {
	if in == nil {
		return nil
	}
	out := new(CacheGroupStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupWorkerOverwrite) DeepCopyInto(out *CacheGroupWorkerOverwrite) {
	*out = *in
	if in.Nodes != nil {
		in, out := &in.Nodes, &out.Nodes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.CacheGroupWorkerTemplate.DeepCopyInto(&out.CacheGroupWorkerTemplate)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupWorkerOverwrite.
func (in *CacheGroupWorkerOverwrite) DeepCopy() *CacheGroupWorkerOverwrite {
	if in == nil {
		return nil
	}
	out := new(CacheGroupWorkerOverwrite)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupWorkerSpec) DeepCopyInto(out *CacheGroupWorkerSpec) {
	*out = *in
	in.Template.DeepCopyInto(&out.Template)
	if in.Overwrite != nil {
		in, out := &in.Overwrite, &out.Overwrite
		*out = make([]CacheGroupWorkerOverwrite, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupWorkerSpec.
func (in *CacheGroupWorkerSpec) DeepCopy() *CacheGroupWorkerSpec {
	if in == nil {
		return nil
	}
	out := new(CacheGroupWorkerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheGroupWorkerTemplate) DeepCopyInto(out *CacheGroupWorkerTemplate) {
	*out = *in
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.HostNetwork != nil {
		in, out := &in.HostNetwork, &out.HostNetwork
		*out = new(bool)
		**out = **in
	}
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.CacheDirs != nil {
		in, out := &in.CacheDirs, &out.CacheDirs
		*out = make([]CacheDir, len(*in))
		copy(*out, *in)
	}
	if in.ImagePullSecrets != nil {
		in, out := &in.ImagePullSecrets, &out.ImagePullSecrets
		*out = make([]corev1.LocalObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.Env != nil {
		in, out := &in.Env, &out.Env
		*out = make([]corev1.EnvVar, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(corev1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.LivenessProbe != nil {
		in, out := &in.LivenessProbe, &out.LivenessProbe
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.ReadinessProbe != nil {
		in, out := &in.ReadinessProbe, &out.ReadinessProbe
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.StartupProbe != nil {
		in, out := &in.StartupProbe, &out.StartupProbe
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.SecurityContext != nil {
		in, out := &in.SecurityContext, &out.SecurityContext
		*out = new(corev1.SecurityContext)
		(*in).DeepCopyInto(*out)
	}
	if in.Lifecycle != nil {
		in, out := &in.Lifecycle, &out.Lifecycle
		*out = new(corev1.Lifecycle)
		(*in).DeepCopyInto(*out)
	}
	if in.VolumeMounts != nil {
		in, out := &in.VolumeMounts, &out.VolumeMounts
		*out = make([]corev1.VolumeMount, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.VolumeDevices != nil {
		in, out := &in.VolumeDevices, &out.VolumeDevices
		*out = make([]corev1.VolumeDevice, len(*in))
		copy(*out, *in)
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]corev1.Volume, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DNSPolicy != nil {
		in, out := &in.DNSPolicy, &out.DNSPolicy
		*out = new(corev1.DNSPolicy)
		**out = **in
	}
	if in.Opts != nil {
		in, out := &in.Opts, &out.Opts
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheGroupWorkerTemplate.
func (in *CacheGroupWorkerTemplate) DeepCopy() *CacheGroupWorkerTemplate {
	if in == nil {
		return nil
	}
	out := new(CacheGroupWorkerTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Condition) DeepCopyInto(out *Condition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Condition.
func (in *Condition) DeepCopy() *Condition {
	if in == nil {
		return nil
	}
	out := new(Condition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Cron) DeepCopyInto(out *Cron) {
	*out = *in
	if in.Suspend != nil {
		in, out := &in.Suspend, &out.Suspend
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Cron.
func (in *Cron) DeepCopy() *Cron {
	if in == nil {
		return nil
	}
	out := new(Cron)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CronSync) DeepCopyInto(out *CronSync) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CronSync.
func (in *CronSync) DeepCopy() *CronSync {
	if in == nil {
		return nil
	}
	out := new(CronSync)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CronSync) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CronSyncList) DeepCopyInto(out *CronSyncList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CronSync, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CronSyncList.
func (in *CronSyncList) DeepCopy() *CronSyncList {
	if in == nil {
		return nil
	}
	out := new(CronSyncList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CronSyncList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CronSyncSpec) DeepCopyInto(out *CronSyncSpec) {
	*out = *in
	if in.Suspend != nil {
		in, out := &in.Suspend, &out.Suspend
		*out = new(bool)
		**out = **in
	}
	if in.SuccessfulJobsHistoryLimit != nil {
		in, out := &in.SuccessfulJobsHistoryLimit, &out.SuccessfulJobsHistoryLimit
		*out = new(int32)
		**out = **in
	}
	if in.FailedJobsHistoryLimit != nil {
		in, out := &in.FailedJobsHistoryLimit, &out.FailedJobsHistoryLimit
		*out = new(int32)
		**out = **in
	}
	in.SyncSpec.DeepCopyInto(&out.SyncSpec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CronSyncSpec.
func (in *CronSyncSpec) DeepCopy() *CronSyncSpec {
	if in == nil {
		return nil
	}
	out := new(CronSyncSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CronSyncStatus) DeepCopyInto(out *CronSyncStatus) {
	*out = *in
	if in.LastScheduleTime != nil {
		in, out := &in.LastScheduleTime, &out.LastScheduleTime
		*out = (*in).DeepCopy()
	}
	if in.NextScheduleTime != nil {
		in, out := &in.NextScheduleTime, &out.NextScheduleTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CronSyncStatus.
func (in *CronSyncStatus) DeepCopy() *CronSyncStatus {
	if in == nil {
		return nil
	}
	out := new(CronSyncStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Metadata) DeepCopyInto(out *Metadata) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Metadata.
func (in *Metadata) DeepCopy() *Metadata {
	if in == nil {
		return nil
	}
	out := new(Metadata)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ParsedSyncSink) DeepCopyInto(out *ParsedSyncSink) {
	*out = *in
	if in.Envs != nil {
		in, out := &in.Envs, &out.Envs
		*out = make([]corev1.EnvVar, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.FilesFrom != nil {
		in, out := &in.FilesFrom, &out.FilesFrom
		*out = new(SyncFilesFrom)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ParsedSyncSink.
func (in *ParsedSyncSink) DeepCopy() *ParsedSyncSink {
	if in == nil {
		return nil
	}
	out := new(ParsedSyncSink)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Policy) DeepCopyInto(out *Policy) {
	*out = *in
	in.Cron.DeepCopyInto(&out.Cron)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Policy.
func (in *Policy) DeepCopy() *Policy {
	if in == nil {
		return nil
	}
	out := new(Policy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Sync) DeepCopyInto(out *Sync) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Sync.
func (in *Sync) DeepCopy() *Sync {
	if in == nil {
		return nil
	}
	out := new(Sync)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Sync) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncFilesFrom) DeepCopyInto(out *SyncFilesFrom) {
	*out = *in
	if in.Files != nil {
		in, out := &in.Files, &out.Files
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ConfigMap != nil {
		in, out := &in.ConfigMap, &out.ConfigMap
		*out = new(corev1.ConfigMapKeySelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncFilesFrom.
func (in *SyncFilesFrom) DeepCopy() *SyncFilesFrom {
	if in == nil {
		return nil
	}
	out := new(SyncFilesFrom)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncList) DeepCopyInto(out *SyncList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Sync, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncList.
func (in *SyncList) DeepCopy() *SyncList {
	if in == nil {
		return nil
	}
	out := new(SyncList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SyncList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSink) DeepCopyInto(out *SyncSink) {
	*out = *in
	if in.External != nil {
		in, out := &in.External, &out.External
		*out = new(SyncSinkExternal)
		(*in).DeepCopyInto(*out)
	}
	if in.JuiceFS != nil {
		in, out := &in.JuiceFS, &out.JuiceFS
		*out = new(SyncSinkJuiceFS)
		(*in).DeepCopyInto(*out)
	}
	if in.JuiceFSCE != nil {
		in, out := &in.JuiceFSCE, &out.JuiceFSCE
		*out = new(SyncSinkJuiceFSCE)
		(*in).DeepCopyInto(*out)
	}
	if in.PVC != nil {
		in, out := &in.PVC, &out.PVC
		*out = new(SyncSinkPVC)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSink.
func (in *SyncSink) DeepCopy() *SyncSink {
	if in == nil {
		return nil
	}
	out := new(SyncSink)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSinkExternal) DeepCopyInto(out *SyncSinkExternal) {
	*out = *in
	in.AccessKey.DeepCopyInto(&out.AccessKey)
	in.SecretKey.DeepCopyInto(&out.SecretKey)
	if in.FilesFrom != nil {
		in, out := &in.FilesFrom, &out.FilesFrom
		*out = new(SyncFilesFrom)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSinkExternal.
func (in *SyncSinkExternal) DeepCopy() *SyncSinkExternal {
	if in == nil {
		return nil
	}
	out := new(SyncSinkExternal)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSinkJuiceFS) DeepCopyInto(out *SyncSinkJuiceFS) {
	*out = *in
	in.Token.DeepCopyInto(&out.Token)
	in.AccessKey.DeepCopyInto(&out.AccessKey)
	in.SecretKey.DeepCopyInto(&out.SecretKey)
	if in.AuthOptions != nil {
		in, out := &in.AuthOptions, &out.AuthOptions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FilesFrom != nil {
		in, out := &in.FilesFrom, &out.FilesFrom
		*out = new(SyncFilesFrom)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSinkJuiceFS.
func (in *SyncSinkJuiceFS) DeepCopy() *SyncSinkJuiceFS {
	if in == nil {
		return nil
	}
	out := new(SyncSinkJuiceFS)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSinkJuiceFSCE) DeepCopyInto(out *SyncSinkJuiceFSCE) {
	*out = *in
	in.MetaPassWord.DeepCopyInto(&out.MetaPassWord)
	if in.FilesFrom != nil {
		in, out := &in.FilesFrom, &out.FilesFrom
		*out = new(SyncFilesFrom)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSinkJuiceFSCE.
func (in *SyncSinkJuiceFSCE) DeepCopy() *SyncSinkJuiceFSCE {
	if in == nil {
		return nil
	}
	out := new(SyncSinkJuiceFSCE)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSinkPVC) DeepCopyInto(out *SyncSinkPVC) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSinkPVC.
func (in *SyncSinkPVC) DeepCopy() *SyncSinkPVC {
	if in == nil {
		return nil
	}
	out := new(SyncSinkPVC)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSinkValue) DeepCopyInto(out *SyncSinkValue) {
	*out = *in
	if in.ValueFrom != nil {
		in, out := &in.ValueFrom, &out.ValueFrom
		*out = new(corev1.EnvVarSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSinkValue.
func (in *SyncSinkValue) DeepCopy() *SyncSinkValue {
	if in == nil {
		return nil
	}
	out := new(SyncSinkValue)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncSpec) DeepCopyInto(out *SyncSpec) {
	*out = *in
	if in.ImagePullSecrets != nil {
		in, out := &in.ImagePullSecrets, &out.ImagePullSecrets
		*out = make([]corev1.LocalObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Affinity != nil {
		in, out := &in.Affinity, &out.Affinity
		*out = new(corev1.Affinity)
		(*in).DeepCopyInto(*out)
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(corev1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.Options != nil {
		in, out := &in.Options, &out.Options
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.From.DeepCopyInto(&out.From)
	in.To.DeepCopyInto(&out.To)
	if in.TTLSecondsAfterFinished != nil {
		in, out := &in.TTLSecondsAfterFinished, &out.TTLSecondsAfterFinished
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncSpec.
func (in *SyncSpec) DeepCopy() *SyncSpec {
	if in == nil {
		return nil
	}
	out := new(SyncSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncStats) DeepCopyInto(out *SyncStats) {
	*out = *in
	if in.LastUpdated != nil {
		in, out := &in.LastUpdated, &out.LastUpdated
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncStats.
func (in *SyncStats) DeepCopy() *SyncStats {
	if in == nil {
		return nil
	}
	out := new(SyncStats)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncStatus) DeepCopyInto(out *SyncStatus) {
	*out = *in
	in.Stats.DeepCopyInto(&out.Stats)
	if in.StartAt != nil {
		in, out := &in.StartAt, &out.StartAt
		*out = (*in).DeepCopy()
	}
	if in.CompletedAt != nil {
		in, out := &in.CompletedAt, &out.CompletedAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncStatus.
func (in *SyncStatus) DeepCopy() *SyncStatus {
	if in == nil {
		return nil
	}
	out := new(SyncStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WarmUp) DeepCopyInto(out *WarmUp) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WarmUp.
func (in *WarmUp) DeepCopy() *WarmUp {
	if in == nil {
		return nil
	}
	out := new(WarmUp)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WarmUp) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WarmUpList) DeepCopyInto(out *WarmUpList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]WarmUp, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WarmUpList.
func (in *WarmUpList) DeepCopy() *WarmUpList {
	if in == nil {
		return nil
	}
	out := new(WarmUpList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WarmUpList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WarmUpSpec) DeepCopyInto(out *WarmUpSpec) {
	*out = *in
	if in.BackoffLimit != nil {
		in, out := &in.BackoffLimit, &out.BackoffLimit
		*out = new(int32)
		**out = **in
	}
	if in.TtlSecondsAfterFinished != nil {
		in, out := &in.TtlSecondsAfterFinished, &out.TtlSecondsAfterFinished
		*out = new(int32)
		**out = **in
	}
	in.Metadata.DeepCopyInto(&out.Metadata)
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Targets != nil {
		in, out := &in.Targets, &out.Targets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Options != nil {
		in, out := &in.Options, &out.Options
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Policy.DeepCopyInto(&out.Policy)
	in.Resources.DeepCopyInto(&out.Resources)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WarmUpSpec.
func (in *WarmUpSpec) DeepCopy() *WarmUpSpec {
	if in == nil {
		return nil
	}
	out := new(WarmUpSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WarmUpStatus) DeepCopyInto(out *WarmUpStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.LastScheduleTime != nil {
		in, out := &in.LastScheduleTime, &out.LastScheduleTime
		*out = (*in).DeepCopy()
	}
	if in.LastCompleteTime != nil {
		in, out := &in.LastCompleteTime, &out.LastCompleteTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WarmUpStatus.
func (in *WarmUpStatus) DeepCopy() *WarmUpStatus {
	if in == nil {
		return nil
	}
	out := new(WarmUpStatus)
	in.DeepCopyInto(out)
	return out
}
