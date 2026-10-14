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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
)

func newLifecycleTestWorker(now time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "worker",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			Annotations:       map[string]string{},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func TestWorkerCounterIdle(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ptr := func(v int64) *int64 { return &v }
	slow, fast := ptr(common.CacheGroupRebalanceIdleRate-1), ptr(common.CacheGroupRebalanceIdleRate)

	tests := []struct {
		name        string
		startAgo    time.Duration
		noStartTime bool
		since       time.Duration
		bytes       *int64
		rate        *int64
		sampleAgo   time.Duration
		notReady    bool
		want        bool
	}{
		{name: "rate below idle rate", since: -5 * time.Minute, bytes: ptr(100), rate: slow, want: true},
		{name: "rate at idle rate", since: -5 * time.Minute, bytes: ptr(100), rate: fast},
		{name: "missing rate", since: -5 * time.Minute, bytes: ptr(100)},
		{name: "zero bytes before minIdle", since: -5 * time.Minute, bytes: ptr(0), rate: ptr(0)},
		{name: "zero bytes after minIdle", since: -11 * time.Minute, bytes: ptr(0), rate: ptr(0), want: true},
		{name: "zero bytes after a process restart", startAgo: 3 * time.Minute, since: -30 * time.Minute, bytes: ptr(0), rate: ptr(0)},
		{name: "missing start time", noStartTime: true, since: -5 * time.Minute, bytes: ptr(100), rate: slow},
		{name: "stale sample", since: -10 * time.Minute, bytes: ptr(100), rate: slow, sampleAgo: workerStatsMaxAge + time.Second},
		{name: "sample before since", since: time.Minute, bytes: ptr(100), rate: slow},
		{name: "worker not ready", since: -5 * time.Minute, bytes: ptr(100), rate: slow, notReady: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := newLifecycleTestWorker(now)
			if tt.notReady {
				worker.Status.Conditions = nil
			}
			sampleAt := metav1.NewTime(now.Add(-tt.sampleAgo))
			stats := juicefsiov1.CacheGroupWorkerStats{Name: worker.Name, LastSampleTime: &sampleAt}
			if !tt.noStartTime {
				startAgo := tt.startAgo
				if startAgo == 0 {
					startAgo = time.Hour
				}
				startTime := metav1.NewTime(now.Add(-startAgo))
				stats.StartTime = &startTime
			}

			if got := workerCounterIdle(worker, stats, tt.bytes, tt.rate, now.Add(tt.since), minLeaveWorkerDuration); got != tt.want {
				t.Fatalf("workerCounterIdle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAddJoinOptionStopsWhenReceiveRateIdle(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	receiveBytes := int64(100)
	expectState := juicefsiov1.CacheGroupWorkerTemplate{Image: "juicedata/mount:ee-5.4.3"}
	idle, busy := common.CacheGroupRebalanceIdleRate-1, common.CacheGroupRebalanceIdleRate

	tests := []struct {
		name          string
		backupAgo     time.Duration
		startAgo      time.Duration // defaults to backupAgo
		otherStartAgo time.Duration // another worker of the group, if set
		delayEnv      string
		rate          int64
		wantJoining   bool
	}{
		{name: "within the rebalance delay", backupAgo: 5 * time.Minute, rate: idle, wantJoining: true},
		{name: "within the rebalance delay margin", backupAgo: 10*time.Minute + 30*time.Second, rate: idle, wantJoining: true},
		{name: "receive rate still high", backupAgo: 15 * time.Minute, rate: busy, wantJoining: true},
		{name: "receive rate idle", backupAgo: 15 * time.Minute, rate: idle},
		{name: "process started late after pulling the image", backupAgo: 15 * time.Minute, startAgo: 8 * time.Minute, rate: idle, wantJoining: true},
		{name: "another worker joined recently", backupAgo: 15 * time.Minute, otherStartAgo: 5 * time.Minute, rate: idle, wantJoining: true},
		{name: "custom rebalance delay", backupAgo: 5 * time.Minute, delayEnv: "1m", rate: idle},
		{name: "invalid rebalance delay falls back to default", backupAgo: 5 * time.Minute, delayEnv: "bad", rate: idle, wantJoining: true},
		{name: "backup duration expired", backupAgo: 2 * time.Hour, rate: busy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := newLifecycleTestWorker(now)
			worker.Annotations[common.AnnoBackupWorker] = now.Add(-tt.backupAgo).Format(time.RFC3339)
			container := corev1.Container{Name: common.WorkerContainerName}
			if tt.delayEnv != "" {
				container.Env = []corev1.EnvVar{{Name: "JFS_CACHE_REBALANCE_DELAY", Value: tt.delayEnv}}
			}
			worker.Spec.Containers = []corev1.Container{container}
			startAgo := tt.startAgo
			if startAgo == 0 {
				startAgo = tt.backupAgo
			}
			rate := tt.rate
			sampleAt := metav1.NewTime(now)
			stats := juicefsiov1.CacheGroupWorkerStats{
				Name:           worker.Name,
				StartTime:      &metav1.Time{Time: now.Add(-startAgo)},
				ReceiveBytes:   &receiveBytes,
				ReceiveRate:    &rate,
				LastSampleTime: &sampleAt,
			}
			cg := &juicefsiov1.CacheGroup{
				Spec:   juicefsiov1.CacheGroupSpec{BackupDuration: &metav1.Duration{Duration: time.Hour}},
				Status: juicefsiov1.CacheGroupStatus{Stats: []juicefsiov1.CacheGroupWorkerStats{stats}},
			}
			if tt.otherStartAgo > 0 {
				cg.Status.Stats = append(cg.Status.Stats, juicefsiov1.CacheGroupWorkerStats{
					Name: "other", StartTime: &metav1.Time{Time: now.Add(-tt.otherStartAgo)},
				})
			}

			r := &CacheGroupReconciler{}
			if got := r.shouldAddJoinOption(context.Background(), cg, worker, expectState, stats); got != tt.wantJoining {
				t.Fatalf("shouldAddJoinOption() = %v, want %v", got, tt.wantJoining)
			}
		})
	}
}

func TestRemoveRedundantWorkersKeepsMigratingWorker(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	replicas := int32(1)

	tests := []struct {
		name        string
		waitingAgo  time.Duration
		wantDeleted bool
	}{
		{name: "migrating worker is kept", waitingAgo: time.Second},
		{name: "migrating worker is deleted after timeout", waitingAgo: 2 * time.Hour, wantDeleted: true},
		{name: "worker not migrating is deleted", wantDeleted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			worker := newLifecycleTestWorker(now)
			worker.Spec.Containers = []corev1.Container{{Name: common.WorkerContainerName, Image: "juicedata/mount:ee-5.4.3"}}
			if tt.waitingAgo > 0 {
				worker.Annotations[common.AnnoWaitingDeleteWorker] = now.Add(-tt.waitingAgo).Format(time.RFC3339)
				// A recreated leaving worker is not ready yet.
				worker.Status.Conditions = nil
			}
			cg := &juicefsiov1.CacheGroup{
				Spec:   juicefsiov1.CacheGroupSpec{Replicas: &replicas},
				Status: juicefsiov1.CacheGroupStatus{ReadyWorker: 1},
			}
			r := &CacheGroupReconciler{Client: fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(worker).Build()}

			if err := r.removeRedundantWorkers(ctx, cg, map[string]juicefsiov1.CacheGroupWorkerTemplate{}, []corev1.Pod{*worker}, nil); err != nil {
				t.Fatal(err)
			}
			err := r.Get(ctx, client.ObjectKeyFromObject(worker), &corev1.Pod{})
			if deleted := apierrors.IsNotFound(err); deleted != tt.wantDeleted {
				t.Fatalf("worker deleted = %v (err: %v), want %v", deleted, err, tt.wantDeleted)
			}
		})
	}
}
