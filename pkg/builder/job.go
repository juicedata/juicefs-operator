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
	"fmt"
	"path"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

type JobBuilder struct {
	wu     *juicefsiov1.WarmUp
	worker *corev1.Pod
}

func NewJobBuilder(wu *juicefsiov1.WarmUp, worker *corev1.Pod) *JobBuilder {
	return &JobBuilder{
		wu:     wu,
		worker: worker,
	}
}

func (j *JobBuilder) NewWarmUpJob() *batchv1.Job {
	job := j.genBaseJob()
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	job.Spec.Template.Spec.Tolerations = j.wu.Spec.Tolerations
	job.Spec.Template.Spec.NodeSelector = j.wu.Spec.NodeSelector
	job.Spec.Template.Spec.ImagePullSecrets = j.worker.Spec.ImagePullSecrets
	job.Spec.Template.Spec.Containers = []corev1.Container{{
		Name:            common.WarmUpContainerName,
		Image:           j.worker.Spec.Containers[0].Image,
		ImagePullPolicy: j.worker.Spec.Containers[0].ImagePullPolicy,
		Command:         j.getWarmUpCommand(),
		Env:             j.worker.Spec.Containers[0].Env,
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
	return job
}

func (j *JobBuilder) NewWarmUpCronJob() *batchv1.CronJob {
	job := j.NewWarmUpJob()

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:            common.GenJobName(j.wu.Name),
			Namespace:       j.wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(j.wu),
			Labels: map[string]string{
				common.LabelAppType: common.LabelCronJobValue,
			},
		},
		Spec: batchv1.CronJobSpec{
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			Schedule:          j.wu.Spec.Policy.Cron.Schedule,
			Suspend:           j.wu.Spec.Policy.Cron.Suspend,
			JobTemplate: batchv1.JobTemplateSpec{
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
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            common.GenJobName(j.wu.Name),
			Namespace:       j.wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(j.wu),
			Labels: map[string]string{
				common.LabelAppType: common.LabelJobValue,
			},
		},
		Spec: batchv1.JobSpec{
			Parallelism:             utils.ToPtr(int32(1)),
			BackoffLimit:            j.wu.Spec.BackoffLimit,
			TTLSecondsAfterFinished: j.wu.Spec.TtlSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      j.wu.Spec.Metadata.Labels,
					Annotations: j.wu.Spec.Metadata.Annotations,
				},
			},
		},
	}
}

func (j *JobBuilder) getWarmUpCommand() []string {
	workerCommad := strings.Split(j.worker.Spec.Containers[0].Command[2], "\n")
	authCmd := workerCommad[0]
	volName, opts := utils.MustParseWorkerMountCmds(workerCommad[1])
	mountOpts := []string{
		"no-sharing",
	}
	for _, opt := range opts {
		if opt == "foreground" {
			continue
		}
		mountOpts = append(mountOpts, opt)
	}
	mountCmds := []string{
		common.JuiceFsMountBinary,
		volName,
		common.MountPoint,
		"-o",
		strings.Join(mountOpts, ","),
	}

	targetsCmd := ""
	cmds := []string{
		"exec",
	}

	if len(j.wu.Spec.Targets) != 0 {
		targets := []string{}
		for _, t := range j.wu.Spec.Targets {
			targets = append(targets, path.Join(common.MountPoint, t))
		}
		targetsCmd = fmt.Sprintf("echo '%s' > /tmp/filelist.txt", strings.Join(targets, "\n"))
		cmds = append(cmds, []string{
			common.JuiceFSBinary,
			"warmup",
			"-f",
			"/tmp/filelist.txt",
		}...)
	} else {
		cmds = append(cmds, []string{
			common.JuiceFSBinary,
			"warmup",
			common.MountPoint,
		}...)
	}

	for _, opt := range j.wu.Spec.Options {
		cmds = append(cmds, fmt.Sprintf("--%s", strings.TrimSpace(opt)))
	}
	return []string{
		"/bin/sh",
		"-c",
		authCmd + "\n" +
			strings.Join(mountCmds, " ") + "\n" +
			targetsCmd + "\n" +
			strings.Join(cmds, " "),
	}
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
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GenCleanCacheJobName(worker.Spec.NodeName),
			Namespace: worker.Namespace,
			Labels: map[string]string{
				common.LabelAppType:    common.LableCleanCacheJobValue,
				common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
			},
		},
		Spec: batchv1.JobSpec{
			Parallelism:             utils.ToPtr(int32(1)),
			BackoffLimit:            utils.ToPtr(int32(3)),
			TTLSecondsAfterFinished: utils.ToPtr(int32(60)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppType:    common.LableCleanCacheJobValue,
						common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
					},
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
