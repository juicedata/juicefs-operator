// Copyright 2026 Juicedata Inc
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
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
)

func (r *CacheGroupReconciler) syncWorkers(ctx context.Context, cg *juicefsiov1.CacheGroup, secret *corev1.Secret,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate, actualWorkers []corev1.Pod,
	workerStats map[string]juicefsiov1.CacheGroupWorkerStats) error {
	log := log.FromContext(ctx)
	updateStrategyType, maxUnavailable := utils.ParseUpdateStrategy(cg.Spec.UpdateStrategy, len(expectStates))
	workers := make(map[string]*corev1.Pod, len(actualWorkers))
	numUnavailable := 0
	for _, worker := range actualWorkers {
		workers[worker.Name] = &worker
		if worker.DeletionTimestamp != nil {
			numUnavailable++
			continue
		}
		if utils.IsPodReady(worker) {
			continue
		}
		numUnavailable++
		if time.Since(worker.CreationTimestamp.Time) > time.Minute {
			conditions := utils.FormatPodNotReadyConditions(worker)
			log.Info("worker is not ready for more than 1 minute", "worker", worker.Name, "since", time.Since(worker.CreationTimestamp.Time).Round(time.Second).String(), "conditions", conditions)
			r.eventf(cg, corev1.EventTypeWarning, "WorkerNotReady", "Worker %s is not ready: %s", worker.Name, strings.Join(conditions, "; "))
		}
	}

	log.V(1).Info("sync worker to expect states", "expect", len(expectStates), "actual", len(actualWorkers), "currentUnavailable", numUnavailable)
	var wg sync.WaitGroup
	errCh := make(chan error, len(expectStates))
	for node, template := range expectStates {
		name := r.getPodName(cg, node)
		actual := workers[name]
		if actual == nil && !utils.CanAddWorker(cg) && !slices.Contains(cg.Status.Rebalance.Members, name) {
			log.Info("waiting for the current join batch before creating worker", "worker", name)
			continue
		}
		addJoinOption := utils.ShouldAddJoinOption(cg, actual, template)
		podBuilder := builder.NewPodBuilder(cg, secret, node, template, addJoinOption)
		expect := podBuilder.NewCacheGroupWorker(ctx, false)
		update := r.workerUpdateAction(ctx, updateStrategyType, podBuilder, expect, actual)
		if update != workerUpToDate && actual != nil && utils.IsPodReady(*actual) {
			if numUnavailable >= maxUnavailable {
				log.V(1).Info("maxUnavailable reached, skip updating worker, waiting for next reconciler", "worker", name)
				update = workerUpToDate
			} else {
				numUnavailable++
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.syncWorker(ctx, cg, template, actual, expect, update, workerStats[name]); err != nil {
				log.Error(err, "failed to sync worker", "worker", name)
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}
	return utilerrors.NewAggregate(errors)
}

func (r *CacheGroupReconciler) syncWorker(ctx context.Context, cg *juicefsiov1.CacheGroup,
	template juicefsiov1.CacheGroupWorkerTemplate, actual, expect *corev1.Pod, update workerUpdate,
	stats juicefsiov1.CacheGroupWorkerStats) error {
	// PVC expansion is independent of whether the Pod needs or is allowed an update.
	if err := r.ensurePVCsForWorker(ctx, cg, expect.Name, template); err != nil {
		return fmt.Errorf("failed to ensure PVCs for worker %s: %w", expect.Name, err)
	}
	if update == workerUpToDate {
		return nil
	}
	if update == workerSmoothRestart {
		if err := r.smoothRestartWorker(ctx, cg, actual, expect); err != nil {
			log.FromContext(ctx).Error(err, "failed to restart worker smoothly, recreate it", "worker", expect.Name)
			update = workerRecreate
		}
	}
	if update == workerRecreate {
		if err := r.createOrUpdateWorker(ctx, cg, actual, expect); err != nil {
			return err
		}
	}
	_, joining := expect.Annotations[common.AnnoBackupWorker]
	r.recordWorkerJoin(ctx, cg, actual, expect, stats, joining)
	return nil
}

// removeRedundantWorkers deletes workers that are no longer expected.
func (r *CacheGroupReconciler) removeRedundantWorkers(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod,
	workerStats map[string]juicefsiov1.CacheGroupWorkerStats) error {
	for _, worker := range actualWorks {
		var workerKey string
		if cg.Spec.Replicas != nil {
			workerKey = worker.Name
		} else {
			workerKey = worker.Spec.NodeName
		}
		// worker may be waiting for scheduler dispatch, waiting for next reconciler
		if workerKey == "" && cg.Spec.EnableScheduling {
			continue
		}
		if _, ok := expectStates[workerKey]; ok {
			continue
		}
		if worker.DeletionTimestamp != nil {
			continue
		}

		canDelete, err := r.gracefulShutdownWorker(ctx, cg, &worker, workerStats[worker.Name])
		if err != nil {
			return err
		}
		if !canDelete {
			continue
		}
		log.FromContext(ctx).Info("deleting redundant worker", "worker", worker.Name)
		if err := r.deleteCacheGroupWorker(ctx, cg, &worker, false); err != nil {
			return err
		}
		if err := r.cleanWorkerCache(ctx, cg, worker); err != nil {
			return err
		}
	}
	return nil
}

// gracefulShutdownWorker migrates the cache of a redundant worker and returns whether it can be deleted.
func (r *CacheGroupReconciler) gracefulShutdownWorker(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	worker *corev1.Pod,
	stats juicefsiov1.CacheGroupWorkerStats) (canDelete bool, err error) {
	if cg.Status.CacheSize == "0B" {
		return true, nil
	}
	v, migrating := worker.Annotations[common.AnnoWaitingDeleteWorker]
	if migrating {
		waitingAt := utils.MustParseTime(v)
		if time.Since(waitingAt) > utils.GetWaitingDeletedMaxDuration(cg.Spec.WaitingDeletedMaxDuration) {
			return true, nil
		}
		if utils.WorkerCounterIdle(worker, stats, stats.TransferBytes, stats.TransferRate, waitingAt, common.MinLeaveWorkerDuration) {
			return true, nil
		}
		if !utils.IsPodReady(*worker) || !utils.IsMountPointReady(ctx, *worker, common.MountPoint) {
			return false, nil
		}
		cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, *worker, common.MountPoint)
		if err != nil {
			return false, err
		}
		return cacheBytes <= 0, nil
	}

	if cg.Status.ReadyWorker <= 1 {
		return true, nil
	}
	if !utils.IsPodReady(*worker) || !utils.IsMountPointReady(ctx, *worker, common.MountPoint) {
		return true, nil
	}
	cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, *worker, common.MountPoint)
	if err != nil {
		return false, err
	}
	if cacheBytes <= 0 {
		return true, nil
	}
	leaving := worker.DeepCopy()
	if utils.WorkerSupportsDecommission(*worker) {
		builder.UpdateWorkerDecommission(leaving)
	} else {
		builder.UpdateWorkerGroupWeight(leaving, 0)
	}
	leaving.Annotations[common.AnnoWaitingDeleteWorker] = time.Now().Format(time.RFC3339)
	if utils.IsWorkerV2(*worker) {
		if err := r.smoothRestartWorker(ctx, cg, worker, leaving); err == nil {
			return false, nil
		} else {
			log.FromContext(ctx).Error(err, "failed to restart worker smoothly during graceful shutdown, recreate it", "worker", worker.Name)
		}
	}
	//recreate worker
	if err := r.deleteCacheGroupWorker(ctx, cg, worker, true); err != nil {
		return false, err
	}
	leaving.ResourceVersion = ""
	if err := r.Create(ctx, leaving); err != nil {
		return false, err
	}
	return false, nil
}

// smoothRestartWorker restarts the juicefs process of a v2 worker with the mount options of target, and then
// updates the juicefs.io annotations of the worker to those of target, leaving others untouched.
func (r *CacheGroupReconciler) smoothRestartWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, worker, target *corev1.Pod) (err error) {
	oldPid, err := utils.TriggerWorkerSmoothRestart(ctx, *worker, target.Annotations[common.AnnoWorkerMountOpts])
	if err != nil {
		return err
	}
	r.eventf(cg, corev1.EventTypeNormal, "WorkerRestartTriggered", "Smooth restart triggered for worker %s", worker.Name)
	if err := utils.WaitForWorkerRestart(ctx, *worker, oldPid); err != nil {
		return err
	}
	updated := worker.DeepCopy()
	isJuiceFSKey := func(k string) bool { return strings.HasPrefix(k, common.AnnotationPrefix) }
	maps.DeleteFunc(updated.Annotations, func(k, _ string) bool { return isJuiceFSKey(k) })
	for k, v := range target.Annotations {
		if isJuiceFSKey(k) {
			updated.Annotations[k] = v
		}
	}
	return r.Patch(ctx, updated, client.MergeFrom(worker))
}

// recordWorkerJoin records join state changes after a worker is successfully created or updated.
func (r *CacheGroupReconciler) recordWorkerJoin(ctx context.Context, cg *juicefsiov1.CacheGroup, actual, expect *corev1.Pod, stats juicefsiov1.CacheGroupWorkerStats, joining bool) {
	log := log.FromContext(ctx)
	var wasJoining bool
	if actual != nil {
		_, wasJoining = actual.Annotations[common.AnnoBackupWorker]
	}
	if joining == wasJoining {
		return
	}
	if joining {
		_, opts := utils.ParseWorkerMountInfo(*expect)
		joinOption := "group-backup"
		if slices.Contains(opts, "commission") {
			joinOption = "commission"
		}
		log.Info("worker join started with "+joinOption, "worker", expect.Name, "option", joinOption)
		r.eventf(cg, corev1.EventTypeNormal, "WorkerJoinStarted", "Worker %s joins with %s", expect.Name, joinOption)
		return
	}
	r.eventf(cg, corev1.EventTypeNormal, "WorkerJoinFinished", "Worker %s exited the temporary join phase, received %s",
		expect.Name, utils.FormatCounter(stats.ReceiveBytes, ""))
}

type workerUpdate int

const (
	workerUpToDate workerUpdate = iota
	workerRecreate
	workerSmoothRestart
	workerInplaceUpdate // TODO
)

func (r *CacheGroupReconciler) workerUpdateAction(ctx context.Context, updateStrategyType appsv1.DaemonSetUpdateStrategyType, podBuilder *builder.PodBuilder, expect, actual *corev1.Pod) workerUpdate {
	if actual == nil {
		return workerRecreate
	}
	if !actual.DeletionTimestamp.IsZero() {
		log.FromContext(ctx).V(1).Info("worker is being deleted, waiting for next reconciler", "worker", actual.Name)
		return workerUpToDate
	}
	if updateStrategyType == appsv1.OnDeleteDaemonSetStrategyType {
		return workerUpToDate
	}
	// pod may be evicted or some exit without restarting., so we need to recreate it
	if actual.Status.Phase == corev1.PodFailed || actual.Status.Phase == corev1.PodUnknown || actual.Status.Phase == corev1.PodSucceeded {
		return workerRecreate
	}
	expectHash := expect.Annotations[common.LabelWorkerHash]
	if !utils.IsWorkerV2(*actual) && utils.IsWorkerV2(*expect) {
		// existing v1 workers are compared with the v1 rendering, so that they are not recreated only for the template
		expectHash = podBuilder.V1WorkerHash(ctx)
	}
	if expectHash != actual.Annotations[common.LabelWorkerHash] {
		return workerRecreate
	}

	// TODO: only resource changes should trigger inplace updates
	// TODO: only annotations/labels changes should trigger inplace updates
	// TODO: only mount options changes should trigger smooth restart

	if utils.IsWorkerV2(*actual) && actual.Annotations[common.AnnoWorkerMountOpts] != expect.Annotations[common.AnnoWorkerMountOpts] {
		return workerSmoothRestart
	}
	return workerUpToDate
}
