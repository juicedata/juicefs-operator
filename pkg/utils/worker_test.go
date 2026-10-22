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
	"testing"
	"time"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCanAddWorker(t *testing.T) {
	now := time.Now()
	openBatch := &juicefsiov1.CacheGroupRebalanceStatus{
		Members: []string{"worker"},
		StartAt: metav1.NewTime(now.Add(2 * common.RebalanceDelayMargin)),
	}
	closingBatch := &juicefsiov1.CacheGroupRebalanceStatus{
		Members: []string{"worker"},
		StartAt: metav1.NewTime(now.Add(common.RebalanceDelayMargin)),
	}
	closedBatch := &juicefsiov1.CacheGroupRebalanceStatus{
		Members: []string{"worker"},
		StartAt: metav1.NewTime(now.Add(common.RebalanceDelayMargin / 2)),
	}
	startedBatch := &juicefsiov1.CacheGroupRebalanceStatus{
		Members: []string{"worker"},
		StartAt: metav1.NewTime(now.Add(-time.Minute)),
	}
	emptyBatch := &juicefsiov1.CacheGroupRebalanceStatus{
		Members: []string{},
		StartAt: metav1.NewTime(now.Add(-time.Minute)),
	}

	tests := []struct {
		name      string
		cacheSize string
		batch     *juicefsiov1.CacheGroupRebalanceStatus
		want      bool
	}{
		{name: "missing cache size respects closed window", batch: closedBatch},
		{name: "zero cache size bypasses closed window", cacheSize: "0B", batch: closedBatch, want: true},
		{name: "small cache respects closed window", cacheSize: "99MiB", batch: closedBatch},
		{name: "one byte cache respects closed window", cacheSize: "1B", batch: closedBatch},
		{name: "no rebalance batch", cacheSize: "1GiB", want: true},
		{name: "empty batch allows join after start time", cacheSize: "1GiB", batch: emptyBatch, want: true},
		{name: "before admission cutoff", cacheSize: "1GiB", batch: openBatch, want: true},
		{name: "admission cutoff reached", cacheSize: "1GiB", batch: closingBatch},
		{name: "within safety margin before rebalance", cacheSize: "1GiB", batch: closedBatch},
		{name: "rebalance already started", cacheSize: "1GiB", batch: startedBatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := &juicefsiov1.CacheGroup{
				Status: juicefsiov1.CacheGroupStatus{CacheSize: tt.cacheSize, Rebalance: tt.batch},
			}
			if got := CanAddWorker(cg); got != tt.want {
				t.Fatalf("CanAddWorker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAddJoinOption_NewWorker(t *testing.T) {
	tests := []struct {
		name           string
		image          string
		cacheSize      string
		backupDuration *metav1.Duration
		opts           []string
		want           bool
	}{
		{name: "missing cache size does not need join option"},
		{name: "zero cache size", cacheSize: "0B"},
		{name: "small cache needs join option", cacheSize: "99MiB", want: true},
		{name: "one byte cache needs join option", cacheSize: "1B", want: true},
		{name: "unsupported image", image: "juicedata/mount:ee-5.0.9", cacheSize: "1GiB"},
		{name: "group-backup supported image", image: "juicedata/mount:ee-5.1.0", cacheSize: "1GiB", want: true},
		{name: "5.2 image needs join option", image: "juicedata/mount:ee-5.2.0", cacheSize: "1GiB", want: true},
		{name: "5.3 image needs join option", image: "juicedata/mount:ee-5.3.0", cacheSize: "1GiB", want: true},
		{name: "commission supported image", image: "juicedata/mount:ee-5.4.4", cacheSize: "1GiB", want: true},
		{name: "zero backup duration", cacheSize: "1GiB", backupDuration: &metav1.Duration{}},
		{name: "one second backup duration", cacheSize: "1GiB", backupDuration: &metav1.Duration{Duration: time.Second}},
		{name: "backup duration above one second", cacheSize: "1GiB", backupDuration: &metav1.Duration{Duration: 2 * time.Second}, want: true},
		{name: "explicit commission", cacheSize: "1GiB", opts: []string{"commission"}},
		{name: "explicit group-backup", cacheSize: "1GiB", opts: []string{"group-backup"}},
		{name: "explicit decommission", cacheSize: "1GiB", opts: []string{"decommission"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := &juicefsiov1.CacheGroup{
				Spec:   juicefsiov1.CacheGroupSpec{BackupDuration: tt.backupDuration},
				Status: juicefsiov1.CacheGroupStatus{CacheSize: tt.cacheSize},
			}
			image := tt.image
			if image == "" {
				image = "juicedata/mount:ee-5.4.4"
			}
			template := juicefsiov1.CacheGroupWorkerTemplate{Image: image, Opts: tt.opts}
			if got := ShouldAddJoinOption(cg, nil, template); got != tt.want {
				t.Fatalf("ShouldAddJoinOption() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAddJoinOption_ExistingWorker(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	joinedAt := now.Add(-15 * time.Minute)
	idleStats := juicefsiov1.CacheGroupWorkerStats{
		Name:           "worker",
		StartTime:      &metav1.Time{Time: joinedAt},
		ReceiveBytes:   ToPtr(int64(100)),
		ReceiveRate:    ToPtr(int64(0)),
		LastSampleTime: &metav1.Time{Time: now},
	}
	busyStats := idleStats
	busyStats.ReceiveRate = ToPtr(common.CacheGroupRebalanceIdleRate)
	slowStats := idleStats
	slowStats.ReceiveRate = ToPtr(common.CacheGroupRebalanceIdleRate - 1)
	fastStats := idleStats
	fastStats.ReceiveRate = ToPtr(common.CacheGroupRebalanceIdleRate + 1)
	zeroBytesStats := idleStats
	zeroBytesStats.ReceiveBytes = ToPtr(int64(0))
	missingBytesStats := idleStats
	missingBytesStats.ReceiveBytes = nil
	missingRateStats := idleStats
	missingRateStats.ReceiveRate = nil
	missingStartStats := idleStats
	missingStartStats.StartTime = nil
	missingSampleStats := idleStats
	missingSampleStats.LastSampleTime = nil
	restartedStats := idleStats
	restartedStats.StartTime = &metav1.Time{Time: now.Add(-5 * time.Minute)}
	staleStats := idleStats
	staleStats.LastSampleTime = &metav1.Time{Time: now.Add(-common.WorkerStatsMaxAge - time.Minute)}
	otherStats := idleStats
	otherStats.Name = "other-worker"
	otherRestartedStats := otherStats
	otherRestartedStats.StartTime = restartedStats.StartTime
	otherBusyStats := otherStats
	otherBusyStats.ReceiveRate = busyStats.ReceiveRate

	tests := []struct {
		name      string
		image     string
		joining   bool
		notReady  bool
		cacheSize string
		stats     []juicefsiov1.CacheGroupWorkerStats
		want      bool
	}{
		{name: "no join annotation does not restart joining", cacheSize: "1GiB", stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}},
		{name: "missing stats keeps join option", joining: true, cacheSize: "1GiB", want: true},
		{name: "another worker stats cannot finish joining", joining: true, cacheSize: "1GiB", stats: []juicefsiov1.CacheGroupWorkerStats{otherStats}, want: true},
		{name: "stale stats cannot finish joining", joining: true, cacheSize: "1GiB", stats: []juicefsiov1.CacheGroupWorkerStats{staleStats}, want: true},
		{name: "not ready worker keeps join option", joining: true, notReady: true, cacheSize: "1GiB", stats: []juicefsiov1.CacheGroupWorkerStats{idleStats}, want: true},
		{name: "zero cache size does not interrupt joining", joining: true, cacheSize: "0B", stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}, want: true},
		{name: "missing cache size does not interrupt joining", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}, want: true},
		{name: "ready worker with fresh idle stats finishes joining", joining: true, cacheSize: "1GiB", stats: []juicefsiov1.CacheGroupWorkerStats{idleStats}},
		{name: "missing receive bytes keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{missingBytesStats}, want: true},
		{name: "missing receive rate keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{missingRateStats}, want: true},
		{name: "missing process start time keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{missingStartStats}, want: true},
		{name: "missing sample time keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{missingSampleStats}, want: true},
		{name: "own restart postpones idle check", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{restartedStats}, want: true},
		{name: "other restart after own stats postpones idle check", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{idleStats, otherRestartedStats}, want: true},
		{name: "other restart before own stats postpones idle check", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{otherRestartedStats, idleStats}, want: true},
		{name: "older peer start does not override own restart", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{restartedStats, otherStats}, want: true},
		{name: "busy peer does not override own idle rate", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{idleStats, otherBusyStats}},
		{name: "idle peer does not override own busy rate", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{busyStats, otherStats}, want: true},
		{name: "receive rate below idle threshold finishes joining", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{slowStats}},
		{name: "receive rate at idle threshold keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}, want: true},
		{name: "receive rate above idle threshold keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{fastStats}, want: true},
		{name: "zero received bytes after delay finishes joining", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{zeroBytesStats}},
		{name: "zero received bytes during restart delay keeps join option", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{zeroBytesStats, otherRestartedStats}, want: true},
		{name: "5.2 image keeps join option while receiving", image: "juicedata/mount:ee-5.2.0", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}, want: true},
		{name: "5.2 image exits joining when idle", image: "juicedata/mount:ee-5.2.0", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{idleStats}},
		{name: "5.3 image keeps join option while receiving", image: "juicedata/mount:ee-5.3.0", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{busyStats}, want: true},
		{name: "5.3 image exits joining when idle", image: "juicedata/mount:ee-5.3.0", joining: true, stats: []juicefsiov1.CacheGroupWorkerStats{idleStats}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			}
			if tt.joining {
				worker.Annotations = map[string]string{common.AnnoBackupWorker: joinedAt.Format(time.RFC3339)}
			}
			if tt.notReady {
				worker.Status.Conditions = nil
			}
			cg := &juicefsiov1.CacheGroup{
				Status: juicefsiov1.CacheGroupStatus{CacheSize: tt.cacheSize, Stats: tt.stats},
			}
			image := tt.image
			if image == "" {
				image = "juicedata/mount:ee-5.4.4"
			}
			template := juicefsiov1.CacheGroupWorkerTemplate{Image: image}
			if got := ShouldAddJoinOption(cg, worker, template); got != tt.want {
				t.Fatalf("ShouldAddJoinOption() = %v, want %v", got, tt.want)
			}
		})
	}
}
