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
	"maps"
	"strings"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
)

// removeRedundantWorkers deletes workers that are no longer expected.
// With more than one ready worker, a ready worker that still has cache blocks is restarted with
// decommission (or group-weight=0 before EE 5.4.0) to migrate its cache, smoothly for v2 workers or
// by recreating otherwise, and deleted once its transfer rate drops below the idle rate or
// WaitingDeletedMaxDuration expires. Otherwise the worker is deleted directly.
func (r *CacheGroupReconciler) removeRedundantWorkers(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod,
	workerStats map[string]juicefsiov1.CacheGroupWorkerStats) error {
	log := log.FromContext(ctx)
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

		// A recreated leaving worker lowers ReadyWorker until it is ready, so keep waiting for its migration.
		_, migrating := worker.Annotations[common.AnnoWaitingDeleteWorker]
		reason := "notEnoughReadyWorkers"
		if cg.Status.ReadyWorker > 1 || migrating {
			var err error
			reason, err = r.gracefulShutdownWorker(ctx, cg, &worker, workerStats[worker.Name])
			if err != nil {
				log.Error(err, "failed to graceful shutdown worker", "worker", worker.Name)
				return err
			}
			if reason == "" {
				continue
			}
		}

		stats := workerStats[worker.Name]
		log.Info("worker leave finished, delete it", "worker", worker.Name, "reason", reason,
			"waitingAt", worker.Annotations[common.AnnoWaitingDeleteWorker],
			"transferBytes", formatCounter(stats.TransferBytes, ""), "transferRate", formatCounter(stats.TransferRate, "/s"))
		if reason == "transferRateIdle" || reason == "noCacheBlocks" {
			r.eventf(cg, corev1.EventTypeNormal, "WorkerLeaveFinished", "Worker %s is deleted (%s), transferred %s",
				worker.Name, reason, formatCounter(stats.TransferBytes, ""))
		} else {
			r.eventf(cg, corev1.EventTypeWarning, "WorkerLeaveFinished", "Worker %s is deleted without finishing data migration (%s)", worker.Name, reason)
		}
		if err := r.deleteCacheGroupWorker(ctx, &worker, false); err != nil {
			log.Error(err, "failed to delete worker", "worker", worker.Name)
			return err
		}
		if err := r.cleanWorkerCache(ctx, cg, worker); err != nil {
			log.Error(err, "failed to clean worker cache", "worker", worker.Name)
			return err
		}
	}
	return nil
}

// gracefulShutdownWorker migrates the cache of a redundant worker. It returns the reason to delete
// the worker now, or "" to keep it.
func (r *CacheGroupReconciler) gracefulShutdownWorker(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	worker *corev1.Pod,
	stats juicefsiov1.CacheGroupWorkerStats) (string, error) {
	log := log.FromContext(ctx)
	v, migrating := worker.Annotations[common.AnnoWaitingDeleteWorker]
	decommission := utils.WorkerSupportsDecommission(*worker)
	if migrating {
		waitingAt := utils.MustParseTime(v)
		if time.Since(waitingAt) > utils.GetWaitingDeletedMaxDuration(cg.Spec.WaitingDeletedMaxDuration) {
			return "waitingDeletedMaxDurationExpired", nil
		}
		if workerCounterIdle(worker, stats, stats.TransferBytes, stats.TransferRate, waitingAt, minLeaveWorkerDuration) {
			return "transferRateIdle", nil
		}
		log.V(1).Info("waiting for worker to leave", "worker", worker.Name, "waitingAt", v,
			"transferBytes", formatCounter(stats.TransferBytes, ""), "transferRate", formatCounter(stats.TransferRate, "/s"))
		if decommission {
			return "", nil
		}
	}

	if !utils.IsPodReady(*worker) || !utils.IsMountPointReady(ctx, *worker, common.MountPoint) {
		if migrating {
			return "", nil
		}
		return "notReady", nil
	}
	cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, *worker, common.MountPoint)
	if err != nil {
		log.Error(err, "failed to get worker cache blocks bytes", "worker", worker.Name)
		return "", err
	}
	if cacheBytes <= 0 {
		return "noCacheBlocks", nil
	}
	if migrating {
		return "", nil
	}

	option := "group-weight=0"
	if decommission {
		option = "decommission"
	}
	log.Info("worker leave started with "+option, "worker", worker.Name, "option", option, "cacheBytes", utils.FormatBytes(int64(cacheBytes)))
	r.eventf(cg, corev1.EventTypeNormal, "WorkerLeaveStarted", "Worker %s leaves with %s, %s cached", worker.Name, option, utils.FormatBytes(int64(cacheBytes)))
	leaving := worker.DeepCopy()
	if decommission {
		builder.UpdateWorkerDecommission(leaving)
	} else {
		builder.UpdateWorkerGroupWeight(leaving, 0)
	}
	leaving.Annotations[common.AnnoWaitingDeleteWorker] = time.Now().Format(time.RFC3339)
	if utils.IsWorkerV2(*worker) {
		err := r.smoothRestartWorker(ctx, worker, leaving)
		if err == nil {
			return "", nil
		}
		log.Error(err, "failed to restart worker smoothly, recreate it", "worker", worker.Name)
	}
	if err := r.deleteCacheGroupWorker(ctx, worker, true); err != nil {
		return "", err
	}
	leaving.ResourceVersion = ""
	if err := r.Create(ctx, leaving); err != nil {
		r.eventf(cg, corev1.EventTypeWarning, "FailedCreateWorker", "Failed to recreate worker %s with %s: %v", worker.Name, option, err)
		return "", err
	}
	return "", nil
}

// smoothRestartWorker restarts the juicefs process of a v2 worker with the mount options of target, and then
// updates the juicefs.io annotations of the worker to those of target, leaving others untouched.
func (r *CacheGroupReconciler) smoothRestartWorker(ctx context.Context, worker, target *corev1.Pod) error {
	if err := utils.SmoothRestartWorker(ctx, *worker, target.Annotations[common.AnnoWorkerMountOpts]); err != nil {
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

const (
	minBackupWorkerDuration = time.Second
	minLeaveWorkerDuration  = 10 * time.Minute
	defaultRebalanceDelay   = 10 * time.Minute
	rebalanceDelayMargin    = time.Minute
)

// shouldAddJoinOption reports whether the worker needs the temporary join option.
func (r *CacheGroupReconciler) shouldAddJoinOption(ctx context.Context, cg *juicefsiov1.CacheGroup, actual *corev1.Pod, expectState juicefsiov1.CacheGroupWorkerTemplate, stats juicefsiov1.CacheGroupWorkerStats) bool {
	log := log.FromContext(ctx)
	if utils.CompareEEImageVersion(expectState.Image, "5.1.0") < 0 {
		return false
	}
	duration := utils.GetBackupWorkerDuration(cg.Spec.BackupDuration)
	if duration <= minBackupWorkerDuration {
		return false
	}
	joinOption := workerJoinOption(expectState.Image)
	if lo.Contains(expectState.Opts, joinOption) || lo.Contains(expectState.Opts, "decommission") {
		return false
	}

	if actual == nil {
		return cg.Status.ReadyWorker >= 1
	}
	v, ok := actual.Annotations[common.AnnoBackupWorker]
	if !ok {
		return false
	}
	backupAt := utils.MustParseTime(v)
	if time.Since(backupAt) >= duration {
		log.Info("worker join finished as backupDuration expired, remove "+joinOption, "worker", actual.Name,
			"backupAt", v, "backupDuration", duration.String())
		return false
	}
	// Existing workers push data to the joining worker only after the group membership has been
	// stable for the rebalance delay, and every worker (re)start changes the membership.
	checkAfter := backupAt
	for _, s := range cg.Status.Stats {
		if s.StartTime != nil && s.StartTime.After(checkAfter) {
			checkAfter = s.StartTime.Time
		}
	}
	checkAfter = checkAfter.Add(workerRebalanceDelay(actual) + rebalanceDelayMargin)
	idleRate := formatCounter(&common.CacheGroupRebalanceIdleRate, "/s")
	if workerCounterIdle(actual, stats, stats.ReceiveBytes, stats.ReceiveRate, checkAfter, 0) {
		log.Info("worker join finished, receive rate dropped below idle rate, remove "+joinOption, "worker", actual.Name,
			"receiveBytes", formatCounter(stats.ReceiveBytes, ""), "receiveRate", formatCounter(stats.ReceiveRate, "/s"), "idleRate", idleRate)
		return false
	}
	if time.Now().Before(checkAfter) {
		log.V(1).Info("worker joining, waiting for the cache group to start rebalancing", "worker", actual.Name,
			"checkAfter", checkAfter.Format(time.RFC3339))
	} else {
		log.V(1).Info("worker joining, waiting for receive rate to drop below idle rate", "worker", actual.Name,
			"receiveBytes", formatCounter(stats.ReceiveBytes, ""), "receiveRate", formatCounter(stats.ReceiveRate, "/s"), "idleRate", idleRate)
	}
	return true
}

func workerJoinOption(image string) string {
	if utils.CompareEEImageVersion(image, common.MinSupportedCommissionVersion) >= 0 {
		return "commission"
	}
	return "group-backup"
}

// recordWorkerJoin logs and records events for the join progress of a worker to be created or updated.
func (r *CacheGroupReconciler) recordWorkerJoin(ctx context.Context, cg *juicefsiov1.CacheGroup, actual, expect *corev1.Pod, stats juicefsiov1.CacheGroupWorkerStats, joining bool) {
	log := log.FromContext(ctx)
	joinOption := workerJoinOption(expect.Spec.Containers[0].Image)
	if joining {
		action := "create"
		if actual != nil {
			action = "recreate"
		}
		log.Info("worker join started, "+action+" it with "+joinOption, "worker", expect.Name, "option", joinOption)
		r.eventf(cg, corev1.EventTypeNormal, "WorkerJoinStarted", "Worker %s joins with %s", expect.Name, joinOption)
		return
	}
	if actual == nil {
		return
	}
	if _, ok := actual.Annotations[common.AnnoBackupWorker]; ok {
		r.eventf(cg, corev1.EventTypeNormal, "WorkerJoinFinished", "Worker %s finished joining, received %s, removing %s",
			expect.Name, formatCounter(stats.ReceiveBytes, ""), joinOption)
	}
}

func formatCounter(v *int64, unit string) string {
	if v == nil {
		return ""
	}
	return utils.FormatBytes(*v) + unit
}

// workerRebalanceDelay returns the cache group rebalance delay of the worker, read the same way as juicefs.
func workerRebalanceDelay(worker *corev1.Pod) time.Duration {
	for _, c := range worker.Spec.Containers {
		if c.Name != common.WorkerContainerName {
			continue
		}
		for _, env := range c.Env {
			if env.Name == "JFS_CACHE_REBALANCE_DELAY" {
				if d, err := time.ParseDuration(env.Value); err == nil {
					return d
				}
			}
		}
	}
	return defaultRebalanceDelay
}

// workerCounterIdle reports whether a byte counter sampled after since moves slower than
// common.CacheGroupRebalanceIdleRate. A zero counter may mean the transfer has not started yet,
// so it also requires the worker process to have run for minIdle after since.
func workerCounterIdle(worker *corev1.Pod, stats juicefsiov1.CacheGroupWorkerStats, bytes, rate *int64, since time.Time, minIdle time.Duration) bool {
	if !utils.IsPodReady(*worker) || bytes == nil || rate == nil || stats.LastSampleTime == nil || stats.StartTime == nil {
		return false
	}
	sampleAt := stats.LastSampleTime.Time
	if time.Since(sampleAt) > workerStatsMaxAge || sampleAt.Before(since) {
		return false
	}
	if *rate >= common.CacheGroupRebalanceIdleRate {
		return false
	}
	if *bytes != 0 {
		return true
	}
	idleSince := since
	if stats.StartTime.Time.After(idleSince) {
		idleSince = stats.StartTime.Time
	}
	return sampleAt.Sub(idleSince) >= minIdle
}

type workerUpdate int

const (
	workerUpToDate workerUpdate = iota
	workerRecreate
	workerSmoothRestart
)

func (r *CacheGroupReconciler) workerUpdateAction(ctx context.Context, updateStrategyType appsv1.DaemonSetUpdateStrategyType, podBuilder *builder.PodBuilder, expect, actual *corev1.Pod) workerUpdate {
	if actual == nil {
		return workerRecreate
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
	if !actual.DeletionTimestamp.IsZero() {
		return workerRecreate
	}
	// Mount options of a v2 worker that are not in its hash, i.e. the join option, are changed by smooth restart.
	if utils.IsWorkerV2(*actual) && actual.Annotations[common.AnnoWorkerMountOpts] != expect.Annotations[common.AnnoWorkerMountOpts] {
		return workerSmoothRestart
	}
	return workerUpToDate
}
