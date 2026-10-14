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

const testContainerID = "containerd://worker-1"

func newLifecycleTestWorker(now time.Time, startedAgo time.Duration) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "worker",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(now.Add(-startedAgo)),
			Annotations:       map[string]string{},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:        common.WorkerContainerName,
				ContainerID: testContainerID,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-startedAgo))},
				},
			}},
		},
	}
}

func TestWorkerCounterIdle(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ptr := func(v int64) *int64 { return &v }
	slow, fast := ptr(common.CacheGroupRebalanceIdleRate-1), ptr(common.CacheGroupRebalanceIdleRate)

	tests := []struct {
		name        string
		startedAgo  time.Duration
		since       time.Duration
		bytes       *int64
		rate        *int64
		sampleAgo   time.Duration
		containerID string
		notReady    bool
		want        bool
	}{
		{name: "rate below idle rate", since: -5 * time.Minute, bytes: ptr(100), rate: slow, want: true},
		{name: "rate at idle rate", since: -5 * time.Minute, bytes: ptr(100), rate: fast},
		{name: "missing rate", since: -5 * time.Minute, bytes: ptr(100)},
		{name: "zero bytes before minIdle", since: -5 * time.Minute, bytes: ptr(0), rate: ptr(0)},
		{name: "zero bytes after minIdle", since: -11 * time.Minute, bytes: ptr(0), rate: ptr(0), want: true},
		{name: "zero bytes after a container restart", startedAgo: 3 * time.Minute, since: -30 * time.Minute, bytes: ptr(0), rate: ptr(0)},
		{name: "stale sample", since: -10 * time.Minute, bytes: ptr(100), rate: slow, sampleAgo: workerStatsMaxAge + time.Second},
		{name: "sample before since", since: time.Minute, bytes: ptr(100), rate: slow},
		{name: "sample from another container", since: -5 * time.Minute, bytes: ptr(100), rate: slow, containerID: "containerd://old"},
		{name: "worker not ready", since: -5 * time.Minute, bytes: ptr(100), rate: slow, notReady: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startedAgo := tt.startedAgo
			if startedAgo == 0 {
				startedAgo = time.Hour
			}
			worker := newLifecycleTestWorker(now, startedAgo)
			if tt.notReady {
				worker.Status.Conditions = nil
			}
			containerID := tt.containerID
			if containerID == "" {
				containerID = testContainerID
			}
			sampleAt := metav1.NewTime(now.Add(-tt.sampleAgo))
			stats := juicefsiov1.CacheGroupWorkerStats{Name: worker.Name, ContainerID: containerID, LastSampleTime: &sampleAt}

			if got := workerCounterIdle(worker, stats, tt.bytes, tt.rate, now.Add(tt.since), minLeaveWorkerDuration); got != tt.want {
				t.Fatalf("workerCounterIdle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAddJoinOptionStopsWhenReceiveRateIdle(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	receiveBytes := int64(100)
	cg := &juicefsiov1.CacheGroup{
		Spec: juicefsiov1.CacheGroupSpec{BackupDuration: &metav1.Duration{Duration: time.Hour}},
	}
	expectState := juicefsiov1.CacheGroupWorkerTemplate{Image: "juicedata/mount:ee-5.4.3"}

	tests := []struct {
		name        string
		backupAgo   time.Duration
		rate        int64
		wantJoining bool
	}{
		{name: "within the minimum join duration", backupAgo: 5 * time.Minute, rate: 0, wantJoining: true},
		{name: "receive rate still high", backupAgo: 15 * time.Minute, rate: common.CacheGroupRebalanceIdleRate, wantJoining: true},
		{name: "receive rate idle", backupAgo: 15 * time.Minute, rate: common.CacheGroupRebalanceIdleRate - 1, wantJoining: false},
		{name: "backup duration expired", backupAgo: 2 * time.Hour, rate: common.CacheGroupRebalanceIdleRate, wantJoining: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := newLifecycleTestWorker(now, tt.backupAgo)
			worker.Annotations[common.AnnoBackupWorker] = now.Add(-tt.backupAgo).Format(time.RFC3339)
			rate := tt.rate
			sampleAt := metav1.NewTime(now)
			stats := juicefsiov1.CacheGroupWorkerStats{
				Name:           worker.Name,
				ContainerID:    testContainerID,
				ReceiveBytes:   &receiveBytes,
				ReceiveRate:    &rate,
				LastSampleTime: &sampleAt,
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
			worker := newLifecycleTestWorker(now, time.Hour)
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
