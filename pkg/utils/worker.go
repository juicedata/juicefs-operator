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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	corev1 "k8s.io/api/core/v1"
)

func IsWorkerV2(worker corev1.Pod) bool {
	return worker.Annotations[common.AnnoWorkerTemplate] == common.WorkerTemplateV2
}

func SupportsRestartArgs(image string) bool {
	return isEEImageAtLeast(image, common.MinSupportedRestartArgsVersion)
}

// CanAddWorker reports whether the cache size and rebalance window allow a new worker.
func CanAddWorker(cg *juicefsiov1.CacheGroup) bool {
	if cg.Status.CacheSize == "0B" {
		return true
	}
	batch := cg.Status.Rebalance
	return batch == nil || len(batch.Members) == 0 ||
		time.Now().Before(batch.StartAt.Add(-common.RebalanceDelayMargin))
}

// ShouldAddJoinOption reports whether to automatically add commission or group-backup.
func ShouldAddJoinOption(cg *juicefsiov1.CacheGroup, actual *corev1.Pod, template juicefsiov1.CacheGroupWorkerTemplate) bool {
	if CompareEEImageVersion(template.Image, "5.1.0") < 0 {
		return false
	}
	duration := GetBackupWorkerDuration(cg.Spec.BackupDuration)
	if duration <= time.Second {
		return false
	}
	if slices.Contains(template.Opts, "commission") || slices.Contains(template.Opts, "group-backup") || slices.Contains(template.Opts, "decommission") {
		return false
	}
	if actual == nil {
		return cg.Status.CacheSize != "" && cg.Status.CacheSize != "0B"
	}
	joinedAt, joining := actual.Annotations[common.AnnoBackupWorker]
	if !joining {
		return false
	}
	joinStartedAt := MustParseTime(joinedAt)
	if time.Since(joinStartedAt) >= duration {
		return false
	}
	// Every worker (re)start can postpone the group's rebalance.
	checkAfter := joinStartedAt
	var stats juicefsiov1.CacheGroupWorkerStats
	for _, s := range cg.Status.Stats {
		if s.Name == actual.Name {
			stats = s
		}
		if s.StartTime != nil && s.StartTime.After(checkAfter) {
			checkAfter = s.StartTime.Time
		}
	}
	checkAfter = checkAfter.Add(WorkerRebalanceDelay(actual) + common.RebalanceDelayMargin)
	return !WorkerCounterIdle(actual, stats, true, checkAfter, 0)
}

// WorkerCounterIdle reports whether a byte counter sampled after since moves slower than
// common.CacheGroupRebalanceIdleRate. A zero counter may mean the transfer has not started yet,
// so it also requires the worker process to have run for minIdle at the sample time.
func WorkerCounterIdle(worker *corev1.Pod, stats juicefsiov1.CacheGroupWorkerStats, isJoin bool, since time.Time, minIdle time.Duration) bool {
	bytes, rate := stats.TransferBytes, stats.TransferRate
	if isJoin {
		bytes, rate = stats.ReceiveBytes, stats.ReceiveRate
	}
	if !IsPodReady(*worker) || bytes == nil || rate == nil || stats.LastSampleTime == nil || stats.StartTime == nil {
		return false
	}
	sampleAt := stats.LastSampleTime.Time
	if time.Since(sampleAt) > common.WorkerStatsMaxAge || sampleAt.Before(since) {
		return false
	}
	if *rate >= common.CacheGroupRebalanceIdleRate {
		return false
	}
	if *bytes != 0 {
		return true
	}
	return sampleAt.Sub(stats.StartTime.Time) >= minIdle
}

// WorkerRebalanceDelay reads the rebalance delay from the worker's JuiceFS environment.
func WorkerRebalanceDelay(worker *corev1.Pod) time.Duration {
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
	return common.DefaultRebalanceDelay
}

func CacheGroupRebalanceDelay(workers []corev1.Pod) time.Duration {
	delay := common.DefaultRebalanceDelay
	for i := range workers {
		d := WorkerRebalanceDelay(&workers[i])
		if i == 0 || d < delay {
			delay = d
		}
	}
	return delay
}

// ParseWorkerMountInfo returns the volume name and mount options of the worker.
func ParseWorkerMountInfo(worker corev1.Pod) (string, []string) {
	volName, opts := MustParseWorkerMountCmds(worker.Spec.Containers[0].Command[2])
	if IsWorkerV2(worker) {
		opts = strings.Split(worker.Annotations[common.AnnoWorkerMountOpts], ",")
	}
	return volName, opts
}

// We have not opened HostPid, so the supervisor is always PID 1
const workerRestartArgsPath = "/tmp/jfs_restart_args.1"

// TriggerWorkerSmoothRestart sends SIGHUP with new mount options and returns the old process ID.
func TriggerWorkerSmoothRestart(ctx context.Context, worker corev1.Pod, mountOpts string) (int, error) {
	oldPid, err := getWorkerPid(ctx, worker)
	if err != nil {
		return 0, err
	}
	volName, _ := ParseWorkerMountInfo(worker)
	args, err := json.Marshal([]string{common.JuiceFsMountBinary, volName, common.MountPoint, "-o", mountOpts})
	if err != nil {
		return 0, err
	}
	script := `umask 077 && printf '%s' "$1" > "$2.tmp" && mv -f "$2.tmp" "$2" && kill -HUP "$3"`
	if _, err := execInWorker(ctx, worker, []string{"sh", "-c", script, "sh", string(args), workerRestartArgsPath, strconv.Itoa(oldPid)}); err != nil {
		return 0, fmt.Errorf("failed to trigger smooth restart: %w", err)
	}
	return oldPid, nil
}

func WaitForWorkerRestart(ctx context.Context, worker corev1.Pod, oldPid int) error {
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		if pid, err := getWorkerPid(ctx, worker); err == nil && pid != oldPid {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for worker to restart")
}

func execInWorker(ctx context.Context, worker corev1.Pod, cmd []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stdout, stderr, err := ExecInPod(ctx, worker.Namespace, worker.Name, common.WorkerContainerName, cmd)
	if err != nil {
		return "", fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func getWorkerPid(ctx context.Context, worker corev1.Pod) (int, error) {
	stdout, err := execInWorker(ctx, worker, []string{"cat", common.MountPoint + "/.config"})
	if err != nil {
		return 0, err
	}
	var conf struct{ Pid int }
	if err := json.Unmarshal([]byte(stdout), &conf); err != nil {
		return 0, fmt.Errorf("failed to parse worker config: %w", err)
	}
	return conf.Pid, nil
}
