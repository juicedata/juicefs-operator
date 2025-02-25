/*
Copyright 2025 Juicedata Inc

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
package builder

import (
	"fmt"
	"strings"

	"maps"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SyncPodEnrtypoint is the entrypoint for sync pod
// has three placeholders for check is distributed, auth command and sync command
const syncPodEnrtypoint = `#!/bin/sh
set -e

IS_DISTRIBUTED=%t
WORKER_IPS=$WORKER_IPS
READ_ONLY_SSH_KEY_PATH=/root/ssh/

if [ "$IS_DISTRIBUTED" = "true" ]; then
	# disable strict host key checking
	sed -i 's/^#\s*\(StrictHostKeyChecking\s*\).*/\1no/' /etc/ssh/ssh_config
	echo "    UserKnownHostsFile /dev/null" >> /etc/ssh/ssh_config
	service ssh start

	if [ ! -d "$READ_ONLY_SSH_KEY_PATH" ]; then
		echo "No ssh key found"
		exit 1
	fi

	mkdir -p /root/.ssh

	for file in $READ_ONLY_SSH_KEY_PATH/*; do
		if [ -f "$file" ]; then
			cat $file > /root/.ssh/$(basename $file)
		fi
	done

	chmod 700 /root/.ssh

	if [ -e /root/.ssh/id_rsa ]; then
		chmod 600  /root/.ssh/id_rsa 
	fi

	if [ -e /root/.ssh/id_rsa.pub ]; then
		chmod 644  /root/.ssh/id_rsa.pub
	fi
fi

# auth if needed
%s

if [ "$IS_DISTRIBUTED" = "true" ]; then
	# copy auth files to workers
	if ls /root/.juicefs/*.conf >/dev/null 2>&1; then
		for ip in $(echo $WORKER_IPS | sed "s/,/ /g"); do
			scp -r -o StrictHostKeyChecking=no /root/.juicefs/*.conf root@$ip:/root/.juicefs
		done
	fi
fi

%s

echo "Sync finished"
`

type SyncPodBuilder struct {
	sc            *juicefsiov1.Sync
	from          *juicefsiov1.ParsedSyncSink
	to            *juicefsiov1.ParsedSyncSink
	workerIPs     []string
	IsDistributed bool
}

func NewSyncPodBuilder(sc *juicefsiov1.Sync, from, to *juicefsiov1.ParsedSyncSink) *SyncPodBuilder {
	return &SyncPodBuilder{
		sc:            sc,
		from:          from,
		to:            to,
		IsDistributed: utils.IsDistributed(sc),
	}
}

func (s *SyncPodBuilder) UpdateWorkerIPs(ips []string) {
	s.workerIPs = ips
}

func (s *SyncPodBuilder) newWorkerPod(i int) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        common.GenSyncWorkerName(s.sc.Name, i),
			Namespace:   s.sc.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				common.LabelSync:    s.sc.Name,
				common.LabelAppType: common.LabelSyncWorkerValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: common.GroupVersion,
					Kind:       common.KindSync,
					Name:       s.sc.Name,
					UID:        s.sc.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										common.LabelSync: s.sc.Name,
									},
								},
								Namespaces:  []string{s.sc.Namespace},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
				},
			},
			NodeSelector:                  s.sc.Spec.NodeSelector,
			Tolerations:                   s.sc.Spec.Tolerations,
			Resources:                     s.sc.Spec.Resources,
			RestartPolicy:                 corev1.RestartPolicyOnFailure,
			TerminationGracePeriodSeconds: utils.ToPtr(int64(0)),
			Containers: []corev1.Container{
				{
					Name:  common.SyncNamePrefix,
					Image: s.sc.Spec.Image,
					Command: []string{
						"sh",
						"-c",
						fmt.Sprintf(syncPodEnrtypoint, s.IsDistributed, "", "sleep infinity"),
					},
				},
			},
		},
	}

	maps.Copy(pod.Labels, s.sc.Spec.Labels)
	maps.Copy(pod.Annotations, s.sc.Spec.Annotations)

	if s.IsDistributed {
		pod.Spec.Containers[0].Ports = append(pod.Spec.Containers[0].Ports, corev1.ContainerPort{
			ContainerPort: 22,
			Name:          "ssh",
		})
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "ssh-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: common.GenSyncSecretName(s.sc.Name),
					Items: []corev1.KeyToPath{
						{
							Key:  "id_rsa.pub",
							Path: "authorized_keys",
						},
						{
							Key:  "id_rsa",
							Path: "id_rsa",
						},
						{
							Key:  "id_rsa.pub",
							Path: "id_rsa.pub",
						},
					},
				},
			},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "ssh-keys",
			MountPath: "/root/ssh",
		})
	}

	return pod
}

func (s *SyncPodBuilder) NewWorkerPods() []corev1.Pod {
	if s.sc.Spec.Replicas == nil || *s.sc.Spec.Replicas <= 1 {
		return nil
	}
	pods := make([]corev1.Pod, 0, int(*s.sc.Spec.Replicas)-1)
	for i := 0; i < int(*s.sc.Spec.Replicas-1); i++ {
		pod := s.newWorkerPod(i + 1)
		pods = append(pods, pod)
	}
	return pods
}

func (s *SyncPodBuilder) genSyncCommands() string {
	cmds := []string{
		"juicefs",
		"sync",
	}

	if len(s.workerIPs) > 0 {
		cmds = append(cmds, "--worker", "$WORKER_IPS")
	}

	for _, opt := range s.sc.Spec.Options {
		opt = strings.TrimPrefix(opt, "--")
		pair := strings.Split(opt, "=")
		if len(pair) == 2 {
			cmds = append(cmds, "--"+pair[0], pair[1])
			continue
		}
		cmds = append(cmds, "--"+pair[0])
	}
	cmds = append(cmds, s.from.Uri, s.to.Uri)
	return strings.Join(cmds, " ")
}

func (s *SyncPodBuilder) genAuthCommand() string {
	cmds := ""
	if s.from.Auth == s.to.Auth {
		return s.from.Auth
	}
	if s.from.Auth != "" {
		cmds += s.from.Auth + "\n"
	}
	if s.to.Auth != "" {
		cmds += s.to.Auth
	}
	return cmds
}

func (s *SyncPodBuilder) genManagerEnvs() []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	if len(s.workerIPs) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "WORKER_IPS",
			Value: strings.Join(s.workerIPs, ","),
		})
	}
	envs = append(envs, s.from.Envs...)
	envs = append(envs, s.to.Envs...)
	return envs
}

func (s *SyncPodBuilder) NewManagerPod() *corev1.Pod {
	managerPod := s.newWorkerPod(0)
	managerPod.Spec.Containers[0].Env = s.genManagerEnvs()
	managerPod.Name = common.GenSyncManagerName(s.sc.Name)
	managerPod.Labels[common.LabelAppType] = common.LabelSyncManagerValue
	managerPod.Spec.Containers[0].Command = []string{
		"sh",
		"-c",
		fmt.Sprintf(
			syncPodEnrtypoint,
			s.IsDistributed,
			s.genAuthCommand(),
			s.genSyncCommands(),
		),
	}
	return &managerPod
}
