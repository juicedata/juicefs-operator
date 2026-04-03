/*
 * Copyright 2024 Juicedata Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package builder

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

type JobBuilder struct {
	wu             *juicefsiov1.WarmUp
	worker         *corev1.Pod
	secret         *corev1.Secret
	cacheGroupName string
}

type warmUpMountInfo struct {
	authCmd   string
	volName   string
	mountOpts []string
}

const (
	warmupFileListPath = "/tmp/filelist.txt"
)

func NewJobBuilder(wu *juicefsiov1.WarmUp, worker *corev1.Pod, secret *corev1.Secret, cacheGroupName string) *JobBuilder {
	return &JobBuilder{
		wu:             wu,
		worker:         worker,
		secret:         secret,
		cacheGroupName: cacheGroupName,
	}
}

func (j *JobBuilder) NewWarmUpJob() *batchv1.Job {
	job := j.genBaseJob()
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	job.Spec.Template.Spec.Tolerations = j.wu.Spec.Tolerations
	job.Spec.Template.Spec.NodeSelector = j.wu.Spec.NodeSelector
	job.Spec.Template.Spec.ImagePullSecrets = j.getImagePullSecrets()
	image := j.getImage()
	job.Spec.Template.Spec.Containers = []corev1.Container{{
		Name:            common.WarmUpContainerName,
		Image:           image,
		ImagePullPolicy: j.getImagePullPolicy(),
		Command:         j.getWarmUpCommand(image),
		Env:             j.getEnvs(),
		SecurityContext: &corev1.SecurityContext{
			Privileged: utils.ToPtr(true),
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"umount " + common.MountPoint,
					},
				},
			},
		},
	}}
	volumes, volumeMounts := j.getWarmupVolumes()
	job.Spec.Template.Spec.Volumes = volumes
	job.Spec.Template.Spec.Containers[0].VolumeMounts = volumeMounts
	return job
}

func (j *JobBuilder) NewWarmUpCronJob() *batchv1.CronJob {
	job := j.NewWarmUpJob()

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:            utils.GenCronJobName(j.wu.Name),
			Namespace:       j.wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(j.wu),
			Labels: map[string]string{
				common.LabelAppType:   common.LabelCronJobValue,
				common.LabelManagedBy: common.LabelManagedByValue,
			},
		},
		Spec: batchv1.CronJobSpec{
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			Schedule:          j.wu.Spec.Policy.Cron.Schedule,
			Suspend:           j.wu.Spec.Policy.Cron.Suspend,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelManagedBy: common.LabelManagedByValue,
					},
				},
				Spec: job.Spec,
			},
		},
	}

	hash := utils.GenHash(cronJob)
	if cronJob.Annotations == nil {
		cronJob.Annotations = make(map[string]string)
	}
	cronJob.Annotations[common.LabelWorkerHash] = hash
	return cronJob
}

func (j *JobBuilder) genBaseJob() *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            utils.GenJobName(j.wu.Name),
			Namespace:       j.wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(j.wu),
			Labels: map[string]string{
				common.LabelAppType:   common.LabelJobValue,
				common.LabelManagedBy: common.LabelManagedByValue,
			},
		},
		Spec: batchv1.JobSpec{
			Parallelism:  utils.ToPtr(int32(1)),
			BackoffLimit: j.wu.Spec.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      j.wu.Spec.Metadata.Labels,
					Annotations: j.wu.Spec.Metadata.Annotations,
				},
			},
		},
	}
	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = make(map[string]string)
	}
	maps.Copy(job.Spec.Template.Labels, map[string]string{
		common.LabelManagedBy: common.LabelManagedByValue,
	})
	return job
}

var (
	ignoreMountOpts = []string{"foreground", "cache-size", "free-space-ratio", "group-weight", "cache-dir", "group-backup", "cache-group", "no-sharing"}
)

func (j *JobBuilder) getImage() string {
	if j.wu.Spec.Image != "" {
		return j.wu.Spec.Image
	}
	if j.worker != nil {
		return j.worker.Spec.Containers[0].Image
	}
	return ""
}

func (j *JobBuilder) getImagePullSecrets() []corev1.LocalObjectReference {
	if j.worker != nil {
		return j.worker.Spec.ImagePullSecrets
	}
	return j.wu.Spec.ImagePullSecrets
}

func (j *JobBuilder) getImagePullPolicy() corev1.PullPolicy {
	if j.worker != nil {
		return j.worker.Spec.Containers[0].ImagePullPolicy
	}
	return corev1.PullIfNotPresent
}

func (j *JobBuilder) getEnvs() []corev1.EnvVar {
	if j.worker != nil {
		return j.worker.Spec.Containers[0].Env
	}

	secretData := utils.ParseSecret(j.secret)
	envs := []corev1.EnvVar{}
	if v, ok := secretData["envs"]; ok && v != "" {
		if secretEnvs, err := utils.ParseYamlOrJson(v); err == nil {
			for k, v := range secretEnvs {
				envs = append(envs, corev1.EnvVar{
					Name:  k,
					Value: fmt.Sprint(v),
				})
			}
		}
	}

	for _, k := range secretStrippedEnvs {
		_, isOptional := secretStrippedEnvOptional[k]
		envs = append(envs, corev1.EnvVar{
			Name: secretStrippedEnvMap[k],
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:      k,
					Optional: &isOptional,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: j.wu.Spec.SecretRef.Name,
					},
				},
			},
		})
	}

	sort.SliceStable(envs, func(i, k int) bool {
		return envs[i].Name < envs[k].Name
	})
	return envs
}

func (j *JobBuilder) getSecretAuthCmd() (string, string) {
	secretData := utils.ParseSecret(j.secret)
	volName := secretData["name"]

	if secretData["initconfig"] != "" {
		return strings.Join([]string{
			"cp",
			path.Join(common.InitConfigMountPath, volName+".conf"),
			"/root/.juicefs",
		}, " "), volName
	}

	authCmds := []string{
		common.JuiceFSBinary,
		"auth",
		volName,
	}

	for _, key := range secretKeys {
		if value, ok := secretData[key]; ok {
			if strippedKey, ok := secretStrippedEnvMap[key]; ok {
				authCmds = append(authCmds, "--"+key, "${"+strippedKey+"}")
			} else {
				authCmds = append(authCmds, "--"+key, value)
			}
		}
	}

	if value, ok := secretData["format-options"]; ok {
		formatOptions := utils.ParseOptions(context.Background(), strings.Split(value, ","))
		for _, opt := range formatOptions {
			if opt[1] != "" {
				authCmds = append(authCmds, "--"+opt[0], opt[1])
			} else {
				authCmds = append(authCmds, "--"+opt[0])
			}
		}
	}

	return strings.Join(authCmds, " "), volName
}

func (j *JobBuilder) getWarmUpMountInfo() warmUpMountInfo {
	info := warmUpMountInfo{
		mountOpts: []string{
			"cache-group=" + j.cacheGroupName,
			"no-sharing",
			"cache-size=0",
		},
	}

	if j.worker != nil {
		workerCommand := strings.Split(j.worker.Spec.Containers[0].Command[2], "\n")
		info.authCmd = workerCommand[0]
		var opts []string
		info.volName, opts = utils.MustParseWorkerMountCmds(workerCommand[1])
		for _, opt := range opts {
			part := strings.SplitN(opt, "=", 2)
			if len(part) < 1 {
				continue
			}
			if slices.Contains(ignoreMountOpts, part[0]) {
				continue
			}
			info.mountOpts = append(info.mountOpts, opt)
		}
	} else {
		info.authCmd, info.volName = j.getSecretAuthCmd()
	}
	info.mountOpts = appendWarmupMountOpts(info.mountOpts, j.wu.Spec.MountOptions)
	return info
}

func (j *JobBuilder) getWarmUpCommand(image string) []string {
	mountInfo := j.getWarmUpMountInfo()
	mountCmds := []string{
		common.JuiceFsMountBinary,
		mountInfo.volName,
		common.MountPoint,
		"-o",
		strings.Join(mountInfo.mountOpts, ","),
	}

	targetsCmd := ""
	cmds := []string{
		"exec",
	}

	if j.wu.Spec.TargetsFrom != nil {
		targets := []string{}
		for _, t := range j.wu.Spec.TargetsFrom.Files {
			targets = append(targets, path.Join(common.MountPoint, t))
		}
		if len(targets) > 0 {
			targetsCmd = fmt.Sprintf("echo '%s' > %s", strings.Join(targets, "\n"), warmupFileListPath)
		}
		// internal file system path
		if j.wu.Spec.TargetsFrom.FilePath != "" {
			targetsCmd = fmt.Sprintf("ln -s %s %s", path.Join(common.MountPoint, j.wu.Spec.TargetsFrom.FilePath), warmupFileListPath)
		}
		// configMap do nothing
	}

	// @deprecated: use targetsFrom.files instead
	if len(j.wu.Spec.Targets) != 0 {
		targets := []string{}
		for _, t := range j.wu.Spec.Targets {
			targets = append(targets, path.Join(common.MountPoint, t))
		}
		if len(targets) > 0 {
			targetsCmd = fmt.Sprintf("echo '%s' > %s", strings.Join(targets, "\n"), warmupFileListPath)
		}
	}

	if j.wu.Spec.TargetsFrom != nil || len(targetsCmd) != 0 {
		cmds = append(cmds, []string{
			common.JuiceFSBinary,
			"warmup",
			"-f",
			warmupFileListPath,
		}...)
	} else {
		// warmup all files
		cmds = append(cmds, []string{
			common.JuiceFSBinary,
			"warmup",
			common.MountPoint,
		}...)
	}
	hasDebug := false
	for _, opt := range j.wu.Spec.Options {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			continue
		}
		parts := strings.SplitN(opt, "=", 2)
		if len(parts) > 0 && parts[0] == "debug" {
			hasDebug = true
		}
		cmds = append(cmds, fmt.Sprintf("--%s", opt))
	}
	// enable debug mode to get warmup stats log
	if utils.WarmupSupportStats(image) && !hasDebug {
		cmds = append(cmds, "--debug")
	}
	return []string{
		"/bin/sh",
		"-c",
		mountInfo.authCmd + "\n" +
			strings.Join(mountCmds, " ") + "\n" +
			targetsCmd + "\n" +
			"cd " + common.MountPoint + "\n" +
			strings.Join(cmds, " "),
	}
}

func appendWarmupMountOpts(base []string, extra []string) []string {
	existing := make(map[string]struct{}, len(base))
	for _, opt := range base {
		part := strings.SplitN(opt, "=", 2)
		if len(part) == 0 || part[0] == "" {
			continue
		}
		existing[part[0]] = struct{}{}
	}

	for _, opt := range utils.ParseOptions(context.Background(), extra) {
		key := opt[0]
		if key == "" || slices.Contains(ignoreMountOpts, key) {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		if opt[1] != "" {
			base = append(base, key+"="+opt[1])
		} else {
			base = append(base, key)
		}
		existing[key] = struct{}{}
	}
	return base
}

func (j *JobBuilder) getWarmupVolumes() ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}
	if j.worker != nil {
		for _, volume := range j.worker.Spec.Volumes {
			if volume.Name == common.InitConfigVolumeName {
				volumes = append(volumes, volume)
				break
			}
		}
		for _, mount := range j.worker.Spec.Containers[0].VolumeMounts {
			if mount.Name == common.InitConfigVolumeName {
				volumeMounts = append(volumeMounts, mount)
				break
			}
		}
	} else {
		secretData := utils.ParseSecret(j.secret)
		volName := secretData["name"]
		if secretData["initconfig"] != "" {
			volumes = append(volumes, corev1.Volume{
				Name: common.InitConfigVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: j.wu.Spec.SecretRef.Name,
						Items: []corev1.KeyToPath{{
							Key:  common.InitConfigVolumeKey,
							Path: volName + ".conf",
						}},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      common.InitConfigVolumeName,
				MountPath: common.InitConfigMountPath,
			})
		}
	}

	if j.wu.Spec.TargetsFrom != nil && j.wu.Spec.TargetsFrom.ConfigMap != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "files-from",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: j.wu.Spec.TargetsFrom.ConfigMap.Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  j.wu.Spec.TargetsFrom.ConfigMap.Key,
							Path: common.SyncFileFromName,
						},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "files-from",
			MountPath: warmupFileListPath,
		})
	}

	// FIXME: we need to mount the config volume for object storage. like ceph
	return volumes, volumeMounts
}

func GetWarmUpOwnerReference(wu *juicefsiov1.WarmUp) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: common.GroupVersion,
		Kind:       common.KindWarmUp,
		Name:       wu.Name,
		UID:        wu.UID,
		Controller: utils.ToPtr(true),
	}}
}

func NewCleanCacheJob(cg juicefsiov1.CacheGroup, worker corev1.Pod) *batchv1.Job {
	cacheVolumes := []corev1.Volume{}
	for _, volume := range worker.Spec.Volumes {
		if strings.HasPrefix(volume.Name, common.CacheDirVolumeNamePrefix) {
			cacheVolumes = append(cacheVolumes, volume)
		}
	}

	if len(cacheVolumes) == 0 {
		return nil
	}

	cacheVolumeMounts := []corev1.VolumeMount{}
	for _, volume := range cacheVolumes {
		cacheVolumeMounts = append(cacheVolumeMounts, corev1.VolumeMount{
			Name:      volume.Name,
			MountPath: fmt.Sprintf("/var/jfsCache/%s", volume.Name),
		})
	}

	podAnnotations := maps.Clone(worker.Annotations)
	podLabels := maps.Clone(worker.Labels)
	podLabels[common.LabelAppType] = common.LabelCleanCacheJobValue

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenCleanCacheJobName(cg.Name, worker.Spec.NodeName),
			Namespace: worker.Namespace,
			Labels: map[string]string{
				common.LabelAppType:    common.LabelCleanCacheJobValue,
				common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
				common.LabelManagedBy:  common.LabelManagedByValue,
			},
		},
		Spec: batchv1.JobSpec{
			Parallelism:             utils.ToPtr(int32(1)),
			BackoffLimit:            utils.ToPtr(int32(3)),
			TTLSecondsAfterFinished: utils.ToPtr(int32(60)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Tolerations:   worker.Spec.Tolerations,
					NodeSelector:  worker.Spec.NodeSelector,
					Containers: []corev1.Container{{
						Name:         common.CleanCacheContainerName,
						Image:        worker.Spec.Containers[0].Image,
						Command:      []string{"/bin/sh", "-c", "rm -rf /var/jfsCache/*/" + cg.Status.FileSystem},
						VolumeMounts: cacheVolumeMounts,
						Resources:    common.DefaultForCleanCacheResources,
					}},
					ServiceAccountName: worker.Spec.ServiceAccountName,
					Volumes:            cacheVolumes,
					NodeName:           worker.Spec.NodeName,
				},
			},
		},
	}
	return job
}
