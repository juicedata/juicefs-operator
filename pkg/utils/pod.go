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

package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func IsPodReady(pod corev1.Pod) bool {
	conditionsTrue := 0
	for _, cond := range pod.Status.Conditions {
		if cond.Status == corev1.ConditionTrue && (cond.Type == corev1.ContainersReady || cond.Type == corev1.PodReady) {
			conditionsTrue++
		}
	}
	return conditionsTrue == 2
}

// IsMountPointReady checks if the mount point is ready in the given pod
func IsMountPointReady(ctx context.Context, pod corev1.Pod, mountPoint string) bool {
	log := log.FromContext(ctx).WithName("checkMountPoint").WithValues("worker", pod.Name)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := []string{"sh", "-c", fmt.Sprintf("stat %s", mountPoint)}

	stdout, stderr, err := ExecInPod(ctx, pod.Namespace, pod.Name, common.WorkerContainerName, cmd)

	if err != nil && len(stderr) == 0 {
		log.Error(err, "failed to execute command")
		return false
	}
	if len(stderr) > 0 {
		log.V(1).Info("mount point is not ready", "stderr", strings.Trim(stderr, "\n"))
		return false
	}
	if !strings.Contains(stdout, "Inode: 1") {
		log.V(1).Info("mount point is not ready")
		return false
	}
	log.Info("mount point is ready")
	return true
}

func GetWorkerCacheBlocksBytes(ctx context.Context, pod corev1.Pod, mountPoint string) (int, error) {
	if !IsPodReady(pod) || !IsMountPointReady(ctx, pod, mountPoint) {
		return 0, fmt.Errorf("pod %s is not ready yet", pod.Name)
	}
	log := log.FromContext(ctx).WithName("getWorkerCacheBlocksBytes").WithValues("worker", pod.Name)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := []string{"sh", "-c", fmt.Sprintf("cat %s/.stats", mountPoint)}

	stdout, stderr, err := ExecInPod(ctx, pod.Namespace, pod.Name, common.WorkerContainerName, cmd)

	if err != nil && len(stderr) == 0 {
		return 0, err
	}
	if len(stderr) > 0 {
		log.Error(err, "failed to get cache blocks bytes", "stderr", strings.Trim(stderr, "\n"))
		return 0, err
	}
	// parse stdout
	// style like: "blockcache.bytes: 0\nblockcache.blocks: 0"
	lines := strings.Split(stdout, "\n")
	cacheBytes := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "blockcache.bytes:") {
			if _, err := fmt.Sscanf(line, "blockcache.bytes: %d", &cacheBytes); err != nil {
				log.Error(err, "failed to parse cache bytes in stats file")
				return 0, err
			}
			break
		}
	}

	return cacheBytes, nil
}

func ParseUpdateStrategy(strategy *appsv1.DaemonSetUpdateStrategy, total int) (appsv1.DaemonSetUpdateStrategyType, int) {
	if strategy == nil {
		return appsv1.RollingUpdateDaemonSetStrategyType, 1
	}

	if strategy.Type == appsv1.OnDeleteDaemonSetStrategyType {
		return appsv1.OnDeleteDaemonSetStrategyType, 1
	}

	if strategy.Type == appsv1.RollingUpdateDaemonSetStrategyType {
		if strategy.RollingUpdate != nil && strategy.RollingUpdate.MaxUnavailable != nil {
			maxUnavailable, err := intstr.GetScaledValueFromIntOrPercent(strategy.RollingUpdate.MaxUnavailable, total, false)
			if err != nil {
				return appsv1.RollingUpdateDaemonSetStrategyType, 1
			}
			return appsv1.RollingUpdateDaemonSetStrategyType, maxUnavailable
		}
		return appsv1.RollingUpdateDaemonSetStrategyType, 1
	}

	return appsv1.RollingUpdateDaemonSetStrategyType, 1
}
