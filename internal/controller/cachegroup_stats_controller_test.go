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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
)

func TestNextWorkerStats(t *testing.T) {
	now := metav1.NewTime(time.Now().Truncate(time.Second))
	ptr := func(v int64) *int64 { return &v }
	previousAgo := func(d time.Duration) juicefsiov1.CacheGroupWorkerStats {
		sampleAt := metav1.NewTime(now.Add(-d))
		return juicefsiov1.CacheGroupWorkerStats{
			ContainerID:    testContainerID,
			TransferBytes:  ptr(60 << 20),
			ReceiveBytes:   ptr(1000),
			LastSampleTime: &sampleAt,
		}
	}
	sample := juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(120 << 20), ReceiveBytes: ptr(1000)}

	tests := []struct {
		name             string
		previous         juicefsiov1.CacheGroupWorkerStats
		containerID      string
		sample           juicefsiov1.CacheGroupWorkerStats
		wantTransferRate *int64
		wantReceiveRate  *int64
	}{
		{name: "first sample", containerID: testContainerID, sample: sample},
		{name: "rates over the sample interval", previous: previousAgo(time.Minute), containerID: testContainerID, sample: sample, wantTransferRate: ptr(1 << 20), wantReceiveRate: ptr(0)},
		{name: "restarted container", previous: previousAgo(time.Minute), containerID: "containerd://new", sample: sample},
		{name: "stale previous sample", previous: previousAgo(workerStatsMaxAge + time.Second), containerID: testContainerID, sample: sample},
		{name: "counter reset", previous: previousAgo(time.Minute), containerID: testContainerID, sample: juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(0), ReceiveBytes: ptr(1060)}, wantReceiveRate: ptr(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextWorkerStats(tt.previous, tt.sample, tt.containerID, now)
			if got.ContainerID != tt.containerID || got.LastSampleTime == nil || !got.LastSampleTime.Equal(&now) {
				t.Fatalf("got containerID %q at %v, want %q at %v", got.ContainerID, got.LastSampleTime, tt.containerID, now)
			}
			assertRate(t, "TransferRate", got.TransferRate, tt.wantTransferRate)
			assertRate(t, "ReceiveRate", got.ReceiveRate, tt.wantReceiveRate)
		})
	}
}

func assertRate(t *testing.T, field string, got, want *int64) {
	t.Helper()
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
}
