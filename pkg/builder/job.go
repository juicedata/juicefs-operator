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

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
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
	job.Spec.Template.Spec.Containers = []corev1.Container{{
		Name:    common.WarmUpContainerName,
		Image:   j.worker.Spec.Containers[0].Image,
		Command: j.getWarmUpCommand(),
	}}
	job.Spec.Template.Spec.ServiceAccountName = common.GenSaName(j.wu.Name)
	return job
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
	cmds := []string{
		"kubectl",
		"-n",
		j.wu.Namespace,
		"exec",
		"-it",
		j.worker.Name,
		"--",
	}

	if len(j.wu.Spec.Targets) != 0 {
		targets := []string{}
		for _, t := range j.wu.Spec.Targets {
			targets = append(targets, path.Join(common.MountPoint, t))
		}
		targetsCmd := fmt.Sprintf("echo '%s' > /tmp/filelist.txt", strings.Join(targets, "\n"))
		cmds = append(cmds, []string{
			targetsCmd,
			"&&",
			common.JuiceFSBinary,
			"warmup",
			"-f",
			"/tmp/filelist.txt",
		}...)
	} else {
		cmds = append(cmds, []string{
			common.JuiceFSBinary,
			"warmup",
			"/mnt/jfs",
		}...)
	}

	for _, opt := range j.wu.Spec.Options {
		cmds = append(cmds, fmt.Sprintf("--%s", strings.TrimSpace(opt)))
	}
	return []string{
		"/bin/sh",
		"-c",
		fmt.Sprintf("#!/bin/bash\nset -e\n%s", strings.Join(cmds, " ")),
	}
}

func GetWarmUpOwnerReference(wu *juicefsiov1.WarmUp) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: "juicefs.io/v1",
		Kind:       "WarmUp",
		Name:       wu.Name,
		UID:        wu.UID,
		Controller: utils.ToPtr(true),
	}}
}
