/*
Copyright 2025 Juicedata Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"reflect"
	"testing"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
)

func TestParseWarmupProgressLog(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected juicefsiov1.WarmUpStats
		wantErr  bool
	}{
		{
			name:    "valid input",
			content: "2025/08/15 14:43:43.782701 juicefs[90011] <DEBUG>: scanned: 3,scannedData: 977 MiB,completed: 3,completedData: 30 KiB,speed: 7.4 KiB/s,failed: 0,failedData: 0 B,uptime: 4s [logProgress@warmup.go:162]",
			expected: juicefsiov1.WarmUpStats{
				Scanned:       3,
				ScannedData:   "977 MiB",
				Completed:     3,
				CompletedData: "30 KiB",
				Failed:        0,
				FailedData:    "0 B",
			},
			wantErr: false,
		},
		{
			name: "valid input need last",
			content: `
			2025/08/15 14:43:40.784487 juicefs[90011] <DEBUG>: scanned: 1,scannedData: 2 MiB,completed: 3,completedData: 0 B,speed: 0 B/s,failed: 0,failedData: 0 B,uptime: 1.001s [logProgress@warmup.go:162]
			2025/08/15 14:43:43.782701 juicefs[90011] <DEBUG>: scanned: 3,scannedData: 977 MiB,completed: 3,completedData: 30 KiB,speed: 7.4 KiB/s,failed: 0,failedData: 0 B,uptime: 4s [logProgress@warmup.go:162]
			`,
			expected: juicefsiov1.WarmUpStats{
				Scanned:       3,
				ScannedData:   "977 MiB",
				Completed:     3,
				CompletedData: "30 KiB",
				Failed:        0,
				FailedData:    "0 B",
			},
			wantErr: false,
		},
		{
			name:     "empty input",
			content:  "",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
		{
			name:     "no scanned line",
			content:  "Some log line\nAnother log line",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
		{
			name:     "invalid scanned format",
			content:  "Some log line\nscanned: abc,scannedData: 10.5 GiB,completed: 50,completedData: 5.2 GiB,speed: 1.0 MiB/s,failed: 10,failedData: 1.1 GiB\nAnother log line",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
		{
			name:     "invalid completed format",
			content:  "Some log line\nscanned: 100,scannedData: 10.5 GiB,completed: abc,completedData: 5.2 GiB,speed: 1.0 MiB/s,failed: 10,failedData: 1.1 GiB\nAnother log line",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
		{
			name:     "invalid failed format",
			content:  "Some log line\nscanned: 100,scannedData: 10.5 GiB,completed: 50,completedData: 5.2 GiB,speed: 1.0 MiB/s,failed: abc,failedData: 1.1 GiB\nAnother log line",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
		{
			name:     "line with scanned keyword but invalid format",
			content:  "Some log line\nscanned: 100 items\nAnother log line",
			expected: juicefsiov1.WarmUpStats{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWarmupProgressLog(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWarmupProgressLog() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParseWarmupProgressLog() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculateWarmupProgress(t *testing.T) {
	tests := []struct {
		name     string
		stats    juicefsiov1.WarmUpStats
		expected string
	}{
		{
			name:     "nil stats",
			stats:    juicefsiov1.WarmUpStats{},
			expected: "0.00%",
		},
		{
			name: "zero scanned data",
			stats: juicefsiov1.WarmUpStats{
				ScannedData:   "0 B",
				CompletedData: "0 B",
			},
			expected: "0.00%",
		},
		{
			name: "half completed",
			stats: juicefsiov1.WarmUpStats{
				ScannedData:   "100 MiB",
				CompletedData: "50 MiB",
			},
			expected: "50.00%",
		},
		{
			name: "fully completed",
			stats: juicefsiov1.WarmUpStats{
				ScannedData:   "100 MiB",
				CompletedData: "100 MiB",
			},
			expected: "100.00%",
		},
		{
			name: "different units",
			stats: juicefsiov1.WarmUpStats{
				ScannedData:   "1 GiB",
				CompletedData: "333 MiB",
			},
			expected: "32.51%",
		},
		{
			name: "small percentage",
			stats: juicefsiov1.WarmUpStats{
				ScannedData:   "1 GiB",
				CompletedData: "1 MiB",
			},
			expected: "0.09%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateWarmupProgress(tt.stats)
			if got != tt.expected {
				t.Errorf("CalculateWarmupProgress() = %v, want %v", got, tt.expected)
			}
		})
	}
}
