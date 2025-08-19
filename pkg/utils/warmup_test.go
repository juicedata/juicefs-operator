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
			content: "2025/08/19 19:08:38.198672 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:144 MiB, speed:60 B/s, failed:10, failed data:2 TiB, uptime:1s [logProgress@warmup.go:162]",
			expected: juicefsiov1.WarmUpStats{
				Scanned:       11,
				ScannedData:   "1.4 GiB",
				Completed:     7,
				CompletedData: "144 MiB",
				Failed:        10,
				FailedData:    "2 TiB",
			},
			wantErr: false,
		},
		{
			name: "valid input need last",
			content: `
				2025/08/19 19:08:38.198672 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:61 B, speed:60 B/s, failed:0, failed data:0 B, uptime:1s [logProgress@warmup.go:162]
				2025/08/19 19:08:39.198976 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:61 B, speed:30 B/s, failed:0, failed data:0 B, uptime:2s [logProgress@warmup.go:162]
				2025/08/19 19:08:40.200869 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:61 B, speed:20 B/s, failed:0, failed data:0 B, uptime:3.002s [logProgress@warmup.go:162]
				2025/08/19 19:08:41.198849 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:144 MiB, speed:36 MiB/s, failed:0, failed data:0 B, uptime:4s [logProgress@warmup.go:162]
				2025/08/19 19:08:42.199217 juicefs[16293] <DEBUG>: scanned:11, scanned data:1.4 GiB, completed:7, completed data:144 MiB, speed:29 MiB/s, failed:0, failed data:0 B, uptime:5s [logProgress@warmup.go:162]
				2025/08/19 19:08:42.933924 juicefs[16293] <DEBUG>: scanned:7, scanned data:1.4 GiB, completed:7, completed data:1.4 GiB, speed:248 MiB/s, failed:0, failed data:0 B, uptime:5.735s [logProgress@warmup.go:162]
			`,
			expected: juicefsiov1.WarmUpStats{
				Scanned:       7,
				ScannedData:   "1.4 GiB",
				Completed:     7,
				CompletedData: "1.4 GiB",
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
