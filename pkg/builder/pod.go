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
	"strings"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func genComands(cg *juicefsiov1.CacheGroup, spec juicefsiov1.CacheGroupWorkerTemplate) []string {
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

	cacheDirs := []string{"/data/jfsCache"}
	cacheGroup := GenCacheGroupName(cg)
	mountCmds := []string{
		"exec",
		common.JuiceFSBinary,
		"mount",
		"${VOL_NAME}",
		"/mnt/jfs",
		"--foreground",
		"--cache-dir",
		strings.Join(cacheDirs, ","),
		"--cache-group",
		cacheGroup,
	}
	if len(spec.Opts) > 0 {
		mountCmds = append(mountCmds, "-o", strings.Join(spec.Opts, ","))
	}
	cmds := []string{
		"sh",
		"-c",
		strings.Join(authCmds, " ") + "\n" + strings.Join(mountCmds, " "),
	}
	return cmds
}

func NewCacheGroupWorker(cg *juicefsiov1.CacheGroup, nodeName string, spec juicefsiov1.CacheGroupWorkerTemplate) *corev1.Pod {
	worker := newBasicPod(cg, nodeName)
	worker.Spec.Affinity = spec.Affinity
	worker.Spec.HostNetwork = spec.HostNetwork
	worker.Spec.Tolerations = spec.Tolerations
	worker.Spec.SchedulerName = spec.SchedulerName
	worker.Spec.ServiceAccountName = spec.ServiceAccountName
	worker.Spec.Containers[0].Image = spec.Image
	worker.Spec.Containers[0].Name = common.WorkerContainerName
	worker.Spec.Containers[0].LivenessProbe = spec.LivenessProbe
	worker.Spec.Containers[0].ReadinessProbe = spec.ReadinessProbe
	worker.Spec.Containers[0].StartupProbe = spec.StartupProbe
	if spec.SecurityContext != nil {
		worker.Spec.Containers[0].SecurityContext = spec.SecurityContext
	} else {
		worker.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Privileged: utils.ToPtr(true),
		}
	}
	worker.Spec.Containers[0].Resources = spec.Resources
	worker.Spec.Containers[0].Env = genEnvs(cg, spec)

	// TODO: cacheDevices
	// TODO: cache-dirs
	worker.Spec.Containers[0].Command = genComands(cg, spec)
	return worker
}
