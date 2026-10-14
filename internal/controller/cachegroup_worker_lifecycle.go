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
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

// removeRedundantWorkers deletes workers that are no longer expected.
// With more than one ready worker, a ready worker that still has cache blocks is recreated with
// decommission (or group-weight=0 before EE 5.4.0) to migrate its cache, and deleted once its
// transfer rate drops below the idle rate or WaitingDeletedMaxDuration expires.
// Otherwise the worker is deleted directly.
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
		if cg.Status.ReadyWorker > 1 || migrating {
			delete, err := r.gracefulShutdownWorker(ctx, cg, &worker, workerStats[worker.Name])
			if err != nil {
				log.Error(err, "failed to graceful shutdown worker", "worker", worker)
				return err
			}
			if !delete {
				continue
			}
		} else {
			log.Info("not enough ready workers to migrate data, delete redundant worker directly", "worker", worker.Name, "readyWorker", cg.Status.ReadyWorker)
		}

		log.Info("found redundant worker, delete it", "worker", worker.Name)
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

// gracefulShutdownWorker migrates the cache of a redundant worker and reports whether it can be deleted now.
func (r *CacheGroupReconciler) gracefulShutdownWorker(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	worker *corev1.Pod,
	stats juicefsiov1.CacheGroupWorkerStats) (bool, error) {
	log := log.FromContext(ctx)
	v, migrating := worker.Annotations[common.AnnoWaitingDeleteWorker]
	decommission := utils.WorkerSupportsDecommission(*worker)
	if migrating {
		waitingAt := utils.MustParseTime(v)
		progress := []any{"worker", worker.Name, "waitingAt", v, "transferBytes", stats.TransferBytes, "transferRate", stats.TransferRate}
		if maxWaiting := utils.GetWaitingDeletedMaxDuration(cg.Spec.WaitingDeletedMaxDuration); time.Since(waitingAt) > maxWaiting {
			log.Info("data migration timed out, delete worker", append(progress, "maxWaiting", maxWaiting.String())...)
			return true, nil
		}
		if workerCounterIdle(worker, stats, stats.TransferBytes, stats.TransferRate, waitingAt, minLeaveWorkerDuration) {
			log.Info("data migration finished, delete worker", progress...)
			return true, nil
		}
		log.V(1).Info("waiting for worker to migrate data", progress...)
		if decommission {
			return false, nil
		}
	}

	if !utils.IsPodReady(*worker) || !utils.IsMountPointReady(ctx, *worker, common.MountPoint) {
		if migrating {
			return false, nil
		}
		log.Info("redundant worker is not ready, delete it without data migration", "worker", worker.Name)
		return true, nil
	}
	cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, *worker, common.MountPoint)
	if err != nil {
		log.Error(err, "failed to get worker cache blocks bytes", "worker", worker.Name)
		return false, err
	}
	if cacheBytes <= 0 {
		log.Info("redundant worker has no cache blocks, delete it", "worker", worker.Name, "migrating", migrating)
		return true, nil
	}
	if migrating {
		return false, nil
	}

	log.Info("start data migration, recreate worker with decommission", "worker", worker.Name, "cacheBytes", cacheBytes, "decommission", decommission)
	if err := r.deleteCacheGroupWorker(ctx, worker, true); err != nil {
		return false, err
	}
	if decommission {
		builder.UpdateWorkerDecommission(worker)
	} else {
		builder.UpdateWorkerGroupWeight(worker, 0)
	}
	worker.ResourceVersion = ""
	worker.Annotations[common.AnnoWaitingDeleteWorker] = time.Now().Format(time.RFC3339)
	return false, r.Create(ctx, worker)
}

const (
	minBackupWorkerDuration = time.Second
	minJoinWorkerDuration   = 10 * time.Minute
	minLeaveWorkerDuration  = 10 * time.Minute
)

func (r *CacheGroupReconciler) shouldAddJoinOption(ctx context.Context, cg *juicefsiov1.CacheGroup, actual *corev1.Pod, expectState juicefsiov1.CacheGroupWorkerTemplate, stats juicefsiov1.CacheGroupWorkerStats) bool {
	if utils.CompareEEImageVersion(expectState.Image, "5.1.0") < 0 {
		return false
	}
	duration := utils.GetBackupWorkerDuration(cg.Spec.BackupDuration)
	if duration <= minBackupWorkerDuration {
		return false
	}
	joinOption := "group-backup"
	if utils.CompareEEImageVersion(expectState.Image, common.MinSupportedCommissionVersion) >= 0 {
		joinOption = "commission"
	}
	if lo.Contains(expectState.Opts, joinOption) || lo.Contains(expectState.Opts, "decommission") {
		return false
	}

	// Enable the temporary join option only when joining an existing cache group.
	if actual == nil {
		return cg.Status.ReadyWorker >= 1
	}
	v, ok := actual.Annotations[common.AnnoBackupWorker]
	if !ok {
		return false
	}
	log := log.FromContext(ctx)
	backupAt := utils.MustParseTime(v)
	progress := []any{"worker", actual.Name, "backupAt", v, "receiveBytes", stats.ReceiveBytes, "receiveRate", stats.ReceiveRate}
	if time.Since(backupAt) >= duration {
		log.Info("worker join finished, backup duration expired, recreate it without join option", append(progress, "backupDuration", duration.String())...)
		return false
	}
	if workerCounterIdle(actual, stats, stats.ReceiveBytes, stats.ReceiveRate, backupAt.Add(minJoinWorkerDuration), 0) {
		log.Info("worker join finished, recreate it without join option", progress...)
		return false
	}
	log.V(1).Info("waiting for joining worker to receive data", progress...)
	return true
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
