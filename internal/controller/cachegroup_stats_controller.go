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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const workerStatsConcurrency = 10

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

	var stats []juicefsiov1.CacheGroupWorkerStats
	var mu sync.Mutex
	workqueue.ParallelizeUntil(ctx, workerStatsConcurrency, len(workers.Items), func(i int) {
		worker := workers.Items[i]
		if !worker.DeletionTimestamp.IsZero() || !utils.IsPodReady(worker) {
			return
		}
		workerStats, err := utils.GetWorkerStats(ctx, worker, common.MountPoint)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to collect worker stats", "worker", worker.Name)
			return
		}
		mu.Lock()
		stats = append(stats, workerStats)
		mu.Unlock()
	})
	sort.Slice(stats, func(i, j int) bool { return stats[i].Name < stats[j].Name })
	if !reflect.DeepEqual(cg.Status.Stats, stats) {
		base := cg.DeepCopy()
		cg.Status.Stats = stats
		if err := r.Status().Patch(ctx, cg, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
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
