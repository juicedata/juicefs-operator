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
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
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
	config := ctrl.GetConfigOrDie()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error(err, "failed to create Kubernetes client")
		return false
	}

	cmd := []string{"sh", "-c", fmt.Sprintf("stat %s", mountPoint)}
	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: common.WorkerContainerName,
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.Error(err, "failed to create SPDY executor")
		return false
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil && stderr.Len() == 0 {
		log.Error(err, "failed to execute command")
		return false
	}
	if stderr.Len() > 0 {
		log.V(1).Info("mount point is not ready", "stderr", strings.Trim(stderr.String(), "\n"))
		return false
	}
	if !strings.Contains(stdout.String(), "Inode: 1") {
		log.V(1).Info("mount point is not ready")
		return false
	}
	log.Info("mount point is ready")
	return true
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
