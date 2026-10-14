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
	at := func(d time.Duration) *metav1.Time { t := metav1.NewTime(now.Add(d)); return &t }
	startTime := at(-time.Hour)
	previousAgo := func(d time.Duration) juicefsiov1.CacheGroupWorkerStats {
		return juicefsiov1.CacheGroupWorkerStats{
			TransferBytes:  ptr(60 << 20),
			ReceiveBytes:   ptr(1000),
			StartTime:      startTime,
			LastSampleTime: at(-d),
		}
	}
	sample := juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(120 << 20), ReceiveBytes: ptr(1000), StartTime: at(-time.Hour + 2*time.Second)}

	tests := []struct {
		name             string
		previous         juicefsiov1.CacheGroupWorkerStats
		sample           juicefsiov1.CacheGroupWorkerStats
		wantTransferRate *int64
		wantReceiveRate  *int64
	}{
		{name: "first sample", sample: sample},
		{name: "rates over the sample interval", previous: previousAgo(time.Minute), sample: sample, wantTransferRate: ptr(1 << 20), wantReceiveRate: ptr(0)},
		{name: "restarted process", previous: previousAgo(time.Minute), sample: juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(120 << 20), StartTime: at(-30 * time.Second)}},
		{name: "missing start time", previous: previousAgo(time.Minute), sample: juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(120 << 20)}},
		{name: "stale previous sample", previous: previousAgo(workerStatsMaxAge + time.Second), sample: sample},
		{name: "counter reset", previous: previousAgo(time.Minute), sample: juicefsiov1.CacheGroupWorkerStats{TransferBytes: ptr(0), ReceiveBytes: ptr(1060), StartTime: startTime}, wantReceiveRate: ptr(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextWorkerStats(tt.previous, tt.sample, now)
			if got.LastSampleTime == nil || !got.LastSampleTime.Equal(&now) {
				t.Fatalf("LastSampleTime = %v, want %v", got.LastSampleTime, now)
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

func TestSumBytes(t *testing.T) {
	gib := int64(1 << 30)
	cacheBytes := func(s juicefsiov1.CacheGroupWorkerStats) *int64 { return s.CacheBytes }
	if got := sumBytes([]juicefsiov1.CacheGroupWorkerStats{{Name: "a"}}, cacheBytes); got != "" {
		t.Fatalf("sumBytes() = %q, want empty when no worker reports the counter", got)
	}
	stats := []juicefsiov1.CacheGroupWorkerStats{{Name: "a", CacheBytes: &gib}, {Name: "b"}, {Name: "c", CacheBytes: &gib}}
	if got := sumBytes(stats, cacheBytes); got != "2.0GiB" {
		t.Fatalf("sumBytes() = %q, want 2.0GiB", got)
	}
}
