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

package utils

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func findJobPods(ctx context.Context, job *batchv1.Job) (*corev1.PodList, error) {
	config := ctrl.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Find pods by job-name label
	labelSelector := fmt.Sprintf("job-name=%s", job.Name)
	podList, err := clientset.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods with label %s: %w", labelSelector, err)
	}

	return podList, nil
}

func WarmupSupportStats(image string) bool {
	return CompareEEImageVersion(image, common.MinSupportedWarmupStatsVersion) >= 0
}

func GetWarmupJobStats(ctx context.Context, job *batchv1.Job) (juicefsiov1.WarmUpStats, string, error) {
	if !WarmupSupportStats(job.Spec.Template.Spec.Containers[0].Image) {
		return juicefsiov1.WarmUpStats{}, "", fmt.Errorf("unsupported image version: %s (minimum required: %s)", job.Spec.Template.Spec.Containers[0].Image, common.MinSupportedWarmupStatsVersion)
	}
	// Find job pod by job-name label
	podList, err := findJobPods(ctx, job)
	if err != nil {
		return juicefsiov1.WarmUpStats{}, "", fmt.Errorf("failed to find job pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return juicefsiov1.WarmUpStats{}, "", fmt.Errorf("no pods found for job %s", job.Name)
	}
	pod := podList.Items[0]

	podCanProvideStats := IsPodSucceeded(pod) || IsPodFailed(pod) || IsPodReady(pod)
	if podCanProvideStats {
		logs, err := LogPod(ctx, pod.Namespace, pod.Name, job.Spec.Template.Spec.Containers[0].Name, 10)
		if err != nil {
			return juicefsiov1.WarmUpStats{}, "", fmt.Errorf("failed to get pod logs: %w", err)
		}
		stats, err := ParseWarmupProgressLog(logs)
		if err != nil {
			return juicefsiov1.WarmUpStats{}, logs, fmt.Errorf("failed to parse warmup progress log: %w", err)
		}
		return stats, logs, nil
	}

	return juicefsiov1.WarmUpStats{}, "", fmt.Errorf("pod %s is not ready (Phase: %s)", pod.Name, pod.Status.Phase)
}

func CalculateWarmupProgress(stats juicefsiov1.WarmUpStats) string {
	if stats.ScannedData == "" || stats.CompletedData == "" {
		return zeroProgress
	}
	total, errTotal := parseBytes(stats.ScannedData)
	complete, errComplete := parseBytes(stats.CompletedData)
	if errTotal != nil || errComplete != nil {
		return zeroProgress
	}
	return CalculateProgress(complete, total)
}

var warmupProgressRegex = regexp.MustCompile(`scanned:\s*(\d+),scannedData:\s*([^,]+),completed:\s*(\d+),completedData:\s*([^,]+),speed:\s*([^,]+),failed:\s*(\d+),failedData:\s*([^,]+)`)

func ParseWarmupProgressLog(content string) (juicefsiov1.WarmUpStats, error) {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.Contains(line, "scanned:") {
			matches := warmupProgressRegex.FindStringSubmatch(line)
			if len(matches) >= 8 {
				scanned := strings.TrimSpace(matches[1])
				scannedData := strings.TrimSpace(matches[2])
				completed := strings.TrimSpace(matches[3])
				completedData := strings.TrimSpace(matches[4])
				failed := strings.TrimSpace(matches[6])
				failedData := strings.TrimSpace(matches[7])

				scannedInt, err := strconv.ParseInt(scanned, 10, 64)
				if err != nil {
					return juicefsiov1.WarmUpStats{}, err
				}
				completedInt, err := strconv.ParseInt(completed, 10, 64)
				if err != nil {
					return juicefsiov1.WarmUpStats{}, err
				}
				failedInt, err := strconv.ParseInt(failed, 10, 64)
				if err != nil {
					return juicefsiov1.WarmUpStats{}, err
				}

				return juicefsiov1.WarmUpStats{
					Scanned:       scannedInt,
					ScannedData:   scannedData,
					Completed:     completedInt,
					CompletedData: completedData,
					Failed:        failedInt,
					FailedData:    failedData,
				}, nil
			}
			break
		}
	}
	return juicefsiov1.WarmUpStats{}, fmt.Errorf("no progress log found")
}
