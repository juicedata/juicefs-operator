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
	"strconv"
	"strings"
	"time"

	"github.com/juicedata/juicefs-operator/pkg/common"
	corev1 "k8s.io/api/core/v1"
)

func IsWorkerV2(worker corev1.Pod) bool {
	return worker.Annotations[common.AnnoWorkerTemplate] == common.WorkerTemplateV2
}

func SupportsSmoothRestart(image string) bool {
	return isEEImageAtLeast(image, common.MinSupportedSmoothRestartVersion)
}

// ParseWorkerMountInfo returns the volume name and mount options of the worker.
func ParseWorkerMountInfo(worker corev1.Pod) (string, []string) {
	volName, opts := MustParseWorkerMountCmds(worker.Spec.Containers[0].Command[2])
	if IsWorkerV2(worker) {
		opts = strings.Split(worker.Annotations[common.AnnoWorkerMountOpts], ",")
	}
	return volName, opts
}

// workerRestartArgsPath is read by the juicefs supervisor to restart the juicefs process with new args on
// SIGHUP.
// The supervisor is always PID 1, since the worker container execs mount.juicefs without hostPID.
const workerRestartArgsPath = "/tmp/jfs_restart_args.1"

// SmoothRestartWorker restarts the juicefs process of the worker with new mount options, keeping the FUSE
// session and the local cache, and waits until the process is restarted.
func SmoothRestartWorker(ctx context.Context, worker corev1.Pod, mountOpts string) error {
	oldPid, err := getWorkerPid(ctx, worker)
	if err != nil {
		return err
	}
	volName, _ := ParseWorkerMountInfo(worker)
	args, err := json.Marshal([]string{common.JuiceFsMountBinary, volName, common.MountPoint, "-o", mountOpts})
	if err != nil {
		return err
	}
	script := `umask 077 && printf '%s' "$1" > "$2.tmp" && mv -f "$2.tmp" "$2" && kill -HUP "$3"`
	if _, err := execInWorker(ctx, worker, []string{"sh", "-c", script, "sh", string(args), workerRestartArgsPath, strconv.Itoa(oldPid)}); err != nil {
		return fmt.Errorf("failed to trigger smooth restart: %w", err)
	}

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
