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
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"time"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	secretKeys = []string{
		"token",
		"access-key",
		"access-key2",
		"bucket",
		"bucket2",
		"subdir",
		"secret-key",
		"secret-key2",
	}

	secretStrippedEnvs = []string{
		"token", "secret-key", "secret-key2",
	}
	secretStrippedEnvMap = map[string]string{
		"token":       "TOKEN",
		"secret-key":  "SECRET_KEY",
		"secret-key2": "SECRET_KEY_2",
	}
	secretStrippedEnvOptional = map[string]bool{
		"secret-key":  true,
		"secret-key2": true,
	}
)

type PodBuilder struct {
	volName              string
	cg                   *juicefsiov1.CacheGroup
	node                 string
	spec                 juicefsiov1.CacheGroupWorkerTemplate
	secretData           map[string]string
	initConfig           string
	groupBackup          bool
	cacheDirsInContainer []string
}

func NewPodBuilder(cg *juicefsiov1.CacheGroup, secret *corev1.Secret, node string, spec juicefsiov1.CacheGroupWorkerTemplate, groupBackup bool) *PodBuilder {
	secretData := utils.ParseSecret(secret)
	initconfig := ""
	if v, ok := secretData["initconfig"]; ok && v != "" {
		initconfig = v
	}
	return &PodBuilder{
		secretData:  secretData,
		volName:     secretData["name"],
		cg:          cg,
		node:        node,
		spec:        spec,
		initConfig:  initconfig,
		groupBackup: groupBackup,
	}
}

func GenCacheGroupName(cg *juicefsiov1.CacheGroup) string {
	if cg.Spec.CacheGroup != "" {
		return cg.Spec.CacheGroup
	}
	return cg.Namespace + "-" + cg.Name
}

func newBasicPod(cg *juicefsiov1.CacheGroup, nodeName string) *corev1.Pod {
	workerName := common.GenWorkerName(cg.Name, nodeName)
	if cg.Spec.Replicas != nil {
		// If replicas is set, nodeName is exactly the worker name.
		workerName = nodeName
	}
	worker := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        workerName,
			Namespace:   cg.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
				common.LabelAppType:    common.LabelWorkerValue,
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
		},
	}
	if cg.Spec.Replicas == nil {
		worker.Spec.NodeName = nodeName
	} else {
		if cg.Spec.Worker.Template.NodeSelector != nil {
			worker.Spec.NodeSelector = cg.Spec.Worker.Template.NodeSelector
		}
		// Add pod anti-affinity to avoid scheduling multiple workers on the same node.
		worker.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      common.LabelCacheGroup,
										Operator: metav1.LabelSelectorOpExists,
									},
								},
							},
							Namespaces:  []string{cg.Namespace},
							TopologyKey: "kubernetes.io/hostname",
						},
						Weight: 100,
					},
				},
			},
		}
	}
	return worker
}

func (p *PodBuilder) genEnvs(ctx context.Context) []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	if v, ok := p.secretData["envs"]; ok && v != "" {
		secretEnvs, err := utils.ParseYamlOrJson(v)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to parse envs, will ignore", "envs", v)
		} else {
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
						Name: p.cg.Spec.SecretRef.Name,
					},
				},
			},
		})
	}
	envs = append(envs, p.spec.Env...)
	sort.SliceStable(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
	return envs
}

func (p *PodBuilder) genAuthCmds(ctx context.Context) []string {
	if p.initConfig != "" {
		conf := p.volName + ".conf"
		authCmds := []string{
			"cp",
			"/etc/juicefs/" + conf,
			"/root/.juicefs",
		}
		return authCmds
	}
	authCmds := []string{
		common.JuiceFSBinary,
		"auth",
		p.volName,
	}

	for _, key := range secretKeys {
		if value, ok := p.secretData[key]; ok {
			if strippedKey, ok := secretStrippedEnvMap[key]; ok {
				authCmds = append(authCmds, "--"+key, "${"+strippedKey+"}")
			} else {
				authCmds = append(authCmds, "--"+key, value)
			}
		}
	}

	// add more options with key `format-options`
	if value, ok := p.secretData["format-options"]; ok {
		formatOptions := utils.ParseOptions(ctx, strings.Split(value, ","))
		for _, opt := range formatOptions {
			if opt[1] != "" {
				authCmds = append(authCmds, "--"+opt[0], opt[1])
			} else {
				authCmds = append(authCmds, "--"+opt[0])
			}
		}
	}

	return authCmds
}

func (p *PodBuilder) genCacheDirs() {
	for i, dir := range p.spec.CacheDirs {
		cachePathInContainer := fmt.Sprintf("%s%d", common.CacheDirVolumeMountPathPrefix, i)
		volumeName := fmt.Sprintf("%s%d", common.CacheDirVolumeNamePrefix, i)
		p.spec.VolumeMounts = append(p.spec.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: cachePathInContainer,
		})
		switch dir.Type {
		case juicefsiov1.CacheDirTypeHostPath:
			p.spec.Volumes = append(p.spec.Volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: dir.Path,
						Type: utils.ToPtr(corev1.HostPathDirectoryOrCreate),
					},
				},
			})
		case juicefsiov1.CacheDirTypePVC:
			p.spec.Volumes = append(p.spec.Volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: dir.Name,
					},
				},
			})
		case juicefsiov1.CacheDirTypeVolumeClaimTemplates:
			pvcName := common.GenPVCName(dir.VolumeClaimTemplate.Name, p.node)
			p.spec.Volumes = append(p.spec.Volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			})
		}
		p.cacheDirsInContainer = append(p.cacheDirsInContainer, cachePathInContainer)
	}

	if len(p.cacheDirsInContainer) == 0 {
		cachePathInContainer := common.DefaultCacheHostPath
		volumeName := fmt.Sprintf("%s%d", common.CacheDirVolumeNamePrefix, 0)
		p.spec.Volumes = append(p.spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: common.DefaultCacheHostPath,
					Type: utils.ToPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		})
		p.spec.VolumeMounts = append(p.spec.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: cachePathInContainer,
		})
		p.cacheDirsInContainer = append(p.cacheDirsInContainer, cachePathInContainer)
	}
}

func (p *PodBuilder) genCommands(ctx context.Context) []string {
	authCmds := p.genAuthCmds(ctx)
	cacheGroup := GenCacheGroupName(p.cg)
	mountCmds := []string{
		"exec",
		common.JuiceFsMountBinary,
		p.volName,
		common.MountPoint,
	}

	opts := []string{
		"foreground",
		"no-update",
		"cache-group=" + cacheGroup,
	}

	parsedOpts := utils.ParseOptions(ctx, p.spec.Opts)
	for _, opt := range parsedOpts {
		if opt[0] == "cache-dir" {
			log.FromContext(ctx).Info("cache-dir option is not allowed, plz use cacheDirs instead")
			continue
		}
		if utils.SliceContains(opts, opt[1]) {
			log.FromContext(ctx).Info("option is duplicated, skip", "option", opt[0])
			continue
		}
		if opt[1] != "" {
			opts = append(opts, strings.TrimSpace(opt[0])+"="+strings.TrimSpace(opt[1]))
		} else {
			opts = append(opts, strings.TrimSpace(opt[0]))
		}
	}
	opts = append(opts, "cache-dir="+strings.Join(p.cacheDirsInContainer, ":"))
	if p.groupBackup {
		opts = append(opts, "group-backup")
	}
	mountCmds = append(mountCmds, "-o", strings.Join(opts, ","))
	cmds := []string{
		"sh",
		"-c",
		strings.Join(authCmds, " ") + "\n" + strings.Join(mountCmds, " "),
	}
	return cmds
}

func (p *PodBuilder) genInitConfigVolumes() {
	if p.initConfig == "" {
		return
	}
	p.spec.Volumes = append(p.spec.Volumes, corev1.Volume{
		Name: common.InitConfigVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: p.cg.Spec.SecretRef.Name,
				Items: []corev1.KeyToPath{
					{
						Key:  common.InitConfigVolumeKey,
						Path: p.volName + ".conf",
					},
				},
			},
		},
	})

	p.spec.VolumeMounts = append(p.spec.VolumeMounts, corev1.VolumeMount{
		Name:      common.InitConfigVolumeName,
		MountPath: common.InitConfigMountPath,
	})
}

func (p *PodBuilder) NewCacheGroupWorker(ctx context.Context) *corev1.Pod {
	worker := newBasicPod(p.cg, p.node)
	p.genInitConfigVolumes()
	p.genCacheDirs()
	spec := p.spec
	if spec.HostNetwork != nil {
		worker.Spec.HostNetwork = *spec.HostNetwork
	} else {
		worker.Spec.HostNetwork = true
	}
	if spec.DNSPolicy != nil {
		worker.Spec.DNSPolicy = *spec.DNSPolicy
	}
	if spec.ImagePullSecrets != nil {
		worker.Spec.ImagePullSecrets = spec.ImagePullSecrets
	} else {
		if common.OperatorPod != nil && common.OperatorPod.Namespace == worker.Namespace {
			worker.Spec.ImagePullSecrets = common.OperatorPod.Spec.ImagePullSecrets
		}
	}
	maps.Copy(worker.Labels, spec.Labels)
	maps.Copy(worker.Annotations, spec.Annotations)
	worker.Spec.Tolerations = spec.Tolerations
	worker.Spec.SchedulerName = spec.SchedulerName
	worker.Spec.ServiceAccountName = spec.ServiceAccountName
	worker.Spec.Volumes = spec.Volumes
	worker.Spec.Containers[0].Image = spec.Image
	worker.Spec.Containers[0].ImagePullPolicy = spec.ImagePullPolicy
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
	worker.Spec.Containers[0].Env = p.genEnvs(ctx)
	worker.Spec.Containers[0].Command = p.genCommands(ctx)

	hash := utils.GenHash(worker)

	// The following fields do not participate in the hash calculation.
	worker.Labels[common.LabelManagedBy] = common.LabelManagedByValue
	worker.Annotations[common.LabelWorkerHash] = hash
	if p.groupBackup {
		backupAt := time.Now().Format(time.RFC3339)
		worker.Annotations[common.AnnoBackupWorker] = backupAt
	}

	return worker
}

func MergeCacheGroupWorkerTemplate(template *juicefsiov1.CacheGroupWorkerTemplate, overwrite juicefsiov1.CacheGroupWorkerOverwrite) {
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
	if overwrite.DNSPolicy != nil {
		template.DNSPolicy = overwrite.DNSPolicy
	}
	if overwrite.CacheDirs != nil {
		template.CacheDirs = overwrite.CacheDirs
	}
}

func UpdateWorkerGroupWeight(worker *corev1.Pod, weight int) {
	cmd := worker.Spec.Containers[0].Command[2]
	weightStr := fmt.Sprint(weight)
	if strings.Contains(cmd, "group-weight") {
		regex := regexp.MustCompile("group-weight=[0-9]+")
		cmd = regex.ReplaceAllString(cmd, "group-weight="+weightStr)
	} else {
		cmd = cmd + ",group-weight=" + weightStr
	}
	worker.Spec.Containers[0].Command[2] = cmd
}
