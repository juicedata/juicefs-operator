// Copyright 2024 Juicedata Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package builder

import (
	"context"
	"strings"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func GenCacheGroupName(cg *juicefsiov1.CacheGroup) string {
	if cg.Spec.CacheGroup != "" {
		return cg.Spec.CacheGroup
	}
	return cg.Namespace + "-" + cg.Name
}

func newBasicPod(cg *juicefsiov1.CacheGroup, nodeName string) *corev1.Pod {
	workerName := common.GenWorkerName(cg.Name, nodeName)
	worker := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        workerName,
			Namespace:   cg.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				common.LabelCacheGroup: cg.Name,
				common.LabelWorker:     workerName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cg.APIVersion,
					Kind:       cg.Kind,
					Name:       cg.Name,
					UID:        cg.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				SecurityContext: &corev1.SecurityContext{
					Privileged: utils.ToPtr(true),
				},
			}},
			NodeName: nodeName,
		},
	}
	return worker
}

func genEnvs(cg *juicefsiov1.CacheGroup, spec juicefsiov1.CacheGroupWorkerTemplate) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{
			Name: "TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "token",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cg.Spec.SecretRef.Name,
					},
				},
			},
		},
		{
			Name: "ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "access-key",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cg.Spec.SecretRef.Name,
					},
				},
			},
		},
		{
			Name: "SECRET_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "secret-key",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cg.Spec.SecretRef.Name,
					},
				},
			},
		},
		{
			Name: "VOL_NAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "name",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cg.Spec.SecretRef.Name,
					},
				},
			},
		},
	}
	envs = append(envs, spec.Env...)
	return envs
}

func genComands(ctx context.Context, cg *juicefsiov1.CacheGroup, spec juicefsiov1.CacheGroupWorkerTemplate) []string {
	authCmds := []string{
		common.JuiceFSBinary,
		"auth",
		"${VOL_NAME}",
		"--token",
		"${TOKEN}",
		"--access-key",
		"${ACCESS_KEY}",
		"--secret-key",
		"${SECRET_KEY}",
	}

	cacheGroup := GenCacheGroupName(cg)
	mountCmds := []string{
		"exec",
		common.JuiceFsMountBinary,
		"${VOL_NAME}",
		common.MountPoint,
	}

	opts := []string{
		"foreground",
		"cache-group=" + cacheGroup,
	}

	cacheDirs := []string{}
	for _, opt := range spec.Opts {
		pair := strings.Split(opt, "=")
		if len(pair) != 2 {
			log.FromContext(ctx).Info("invalid opt", "opt", opt, "cg", cg.Name)
			continue
		}
		// TODO: validate cache-dir
		if pair[0] == "cache-dir" {
			cacheDirs = strings.Split(pair[1], ":")
			continue
		}
		opts = append(opts, strings.TrimSpace(pair[0])+"="+strings.TrimSpace(pair[1]))
	}
	if len(cacheDirs) == 0 {
		cacheDirs = append(cacheDirs, "/var/jfsCache")
	}
	opts = append(opts, "cache-dir="+strings.Join(cacheDirs, ":"))
	mountCmds = append(mountCmds, "-o", strings.Join(opts, ","))
	cmds := []string{
		"sh",
		"-c",
		strings.Join(authCmds, " ") + "\n" + strings.Join(mountCmds, " "),
	}
	return cmds
}

func NewCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, nodeName string, spec juicefsiov1.CacheGroupWorkerTemplate) *corev1.Pod {
	worker := newBasicPod(cg, nodeName)
	if spec.HostNetwork != nil {
		worker.Spec.HostNetwork = *spec.HostNetwork
	} else {
		worker.Spec.HostNetwork = true
	}
	worker.Spec.Tolerations = spec.Tolerations
	worker.Spec.SchedulerName = spec.SchedulerName
	worker.Spec.ServiceAccountName = spec.ServiceAccountName
	worker.Spec.Volumes = spec.Volumes
	worker.Spec.Containers[0].Image = spec.Image
	worker.Spec.Containers[0].Name = common.WorkerContainerName
	worker.Spec.Containers[0].LivenessProbe = spec.LivenessProbe
	worker.Spec.Containers[0].ReadinessProbe = spec.ReadinessProbe
	worker.Spec.Containers[0].StartupProbe = spec.StartupProbe
	worker.Spec.Containers[0].VolumeMounts = spec.VolumeMounts
	worker.Spec.Containers[0].VolumeDevices = spec.VolumeDevices
	if spec.SecurityContext != nil {
		worker.Spec.Containers[0].SecurityContext = spec.SecurityContext
	} else {
		worker.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Privileged: utils.ToPtr(true),
		}
	}
	if spec.Lifecycle != nil {
		worker.Spec.Containers[0].Lifecycle = spec.Lifecycle
	} else {
		worker.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"sh",
						"-c",
						"umount " + common.MountPoint,
					},
				},
			},
		}
	}
	if spec.Resources != nil {
		worker.Spec.Containers[0].Resources = *spec.Resources
	} else {
		worker.Spec.Containers[0].Resources = common.DefaultResources
	}
	worker.Spec.Containers[0].Env = genEnvs(cg, spec)
	worker.Spec.Containers[0].Command = genComands(ctx, cg, spec)
	return worker
}

func MergeCacheGrouopWorkerTemplate(template *juicefsiov1.CacheGroupWorkerTemplate, overwrite juicefsiov1.CacheGroupWorkerOverwrite) {
	if overwrite.ServiceAccountName != "" {
		template.ServiceAccountName = overwrite.ServiceAccountName
	}
	if overwrite.HostNetwork != nil {
		template.HostNetwork = overwrite.HostNetwork
	}
	if overwrite.SchedulerName != "" {
		template.SchedulerName = overwrite.SchedulerName
	}
	if overwrite.Tolerations != nil {
		template.Tolerations = overwrite.Tolerations
	}
	if overwrite.Image != "" {
		template.Image = overwrite.Image
	}
	if overwrite.Env != nil {
		template.Env = overwrite.Env
	}
	if overwrite.Resources != nil {
		template.Resources = overwrite.Resources
	}
	if overwrite.LivenessProbe != nil {
		template.LivenessProbe = overwrite.LivenessProbe
	}
	if overwrite.ReadinessProbe != nil {
		template.ReadinessProbe = overwrite.ReadinessProbe
	}
	if overwrite.StartupProbe != nil {
		template.StartupProbe = overwrite.StartupProbe
	}
	if overwrite.SecurityContext != nil {
		template.SecurityContext = overwrite.SecurityContext
	}
	if overwrite.Lifecycle != nil {
		template.Lifecycle = overwrite.Lifecycle
	}
	if overwrite.VolumeMounts != nil {
		template.VolumeMounts = overwrite.VolumeMounts
	}
	if overwrite.VolumeDevices != nil {
		template.VolumeDevices = overwrite.VolumeDevices
	}
	if overwrite.Volumes != nil {
		template.Volumes = overwrite.Volumes
	}
	if overwrite.Opts != nil {
		template.Opts = overwrite.Opts
	}
}
