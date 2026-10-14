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

package controller

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"time"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	workerStatsConcurrency = 10
	workerStatsMinInterval = 30 * time.Second
	workerStatsMaxAge      = 2 * time.Minute
)

type CacheGroupStatsReconciler struct {
	client.Client
}

func (r *CacheGroupStatsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cg := &juicefsiov1.CacheGroup{}
	if err := r.Get(ctx, req.NamespacedName, cg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !cg.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	workers := &corev1.PodList{}
	if err := r.List(ctx, workers, client.InNamespace(cg.Namespace), client.MatchingLabels{
		common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
		common.LabelAppType:    common.LabelWorkerValue,
	}); err != nil {
		return ctrl.Result{}, err
	}

	previousStats := lo.KeyBy(cg.Status.Stats, func(s juicefsiov1.CacheGroupWorkerStats) string { return s.Name })
	var stats []juicefsiov1.CacheGroupWorkerStats
	var mu sync.Mutex
	workqueue.ParallelizeUntil(ctx, workerStatsConcurrency, len(workers.Items), func(i int) {
		worker := workers.Items[i]
		if !worker.DeletionTimestamp.IsZero() {
			return
		}
		containerID := workerContainerStatus(worker).ContainerID
		// On failure the previous sample is kept and ages out.
		next, ok := previousStats[worker.Name]
		recent := ok && next.ContainerID == containerID && next.LastSampleTime != nil &&
			time.Since(next.LastSampleTime.Time) < workerStatsMinInterval
		if !recent && utils.IsPodReady(worker) {
			if sample, err := utils.GetWorkerStats(ctx, worker, common.MountPoint); err != nil {
				log.FromContext(ctx).Error(err, "failed to collect worker stats", "worker", worker.Name)
			} else {
				next, ok = nextWorkerStats(next, sample, containerID, metav1.Now()), true
			}
		}
		if !ok {
			return
		}
		mu.Lock()
		stats = append(stats, next)
		mu.Unlock()
	})
	sort.Slice(stats, func(i, j int) bool { return stats[i].Name < stats[j].Name })
	cacheSize := totalCacheSize(stats)
	if !reflect.DeepEqual(cg.Status.Stats, stats) || cg.Status.CacheSize != cacheSize {
		base := cg.DeepCopy()
		cg.Status.Stats = stats
		cg.Status.CacheSize = cacheSize
		if err := r.Status().Patch(ctx, cg, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// nextWorkerStats derives each counter's rate from the previous sample of the same container.
func nextWorkerStats(previous, sample juicefsiov1.CacheGroupWorkerStats, containerID string, now metav1.Time) juicefsiov1.CacheGroupWorkerStats {
	sample.ContainerID = containerID
	sample.LastSampleTime = &now
	if previous.LastSampleTime == nil || containerID == "" || previous.ContainerID != containerID {
		return sample
	}
	interval := now.Sub(previous.LastSampleTime.Time)
	if interval <= 0 || interval > workerStatsMaxAge {
		return sample
	}
	rate := func(prevBytes, bytes *int64) *int64 {
		if prevBytes == nil || bytes == nil || *bytes < *prevBytes {
			return nil
		}
		r := int64(float64(*bytes-*prevBytes) / interval.Seconds())
		return &r
	}
	sample.TransferRate = rate(previous.TransferBytes, sample.TransferBytes)
	sample.ReceiveRate = rate(previous.ReceiveBytes, sample.ReceiveBytes)
	return sample
}

// totalCacheSize returns the total cache size of all workers, or "" if no worker reports it.
func totalCacheSize(stats []juicefsiov1.CacheGroupWorkerStats) string {
	var total int64
	reported := false
	for _, s := range stats {
		if s.CacheBytes != nil {
			total += *s.CacheBytes
			reported = true
		}
	}
	if !reported {
		return ""
	}
	return utils.FormatBytes(total)
}

func workerContainerStatus(worker corev1.Pod) corev1.ContainerStatus {
	for _, c := range worker.Status.ContainerStatuses {
		if c.Name == common.WorkerContainerName {
			return c
		}
	}
	return corev1.ContainerStatus{}
}

func (r *CacheGroupStatsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("cachegroup-stats").
		For(&juicefsiov1.CacheGroup{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxCGConcurrentReconciles,
		}).
		Complete(r)
}
