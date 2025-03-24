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
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestGenSshKeys(t *testing.T) {
	privateKey, publicKey, err := GenerateSSHKeyPair()
	if err != nil {
		t.Errorf("GenSshKeys() error = %v", err)
		return
	}
	if len(privateKey) == 0 {
		t.Errorf("GenSshKeys() privateKey = %v", privateKey)
	}
	if len(publicKey) == 0 {
		t.Errorf("GenSshKeys() publicKey = %v", publicKey)
	}
}

func TestParseSyncSink(t *testing.T) {
	tests := []struct {
		name     string
		sink     juicefsiov1.SyncSink
		syncName string
		ref      string
		want     *juicefsiov1.ParsedSyncSink
		wantErr  bool
	}{
		{
			name: "External sink",
			sink: juicefsiov1.SyncSink{
				External: &juicefsiov1.SyncSinkExternal{
					Uri: "oss://example.com",
					AccessKey: juicefsiov1.SyncSinkValue{
						Value: "accessKey",
					},
					SecretKey: juicefsiov1.SyncSinkValue{
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "other_secret",
								},
								Key: "secretKey",
							},
						},
					},
				},
			},
			syncName: "sync1",
			ref:      "FROM",
			want: &juicefsiov1.ParsedSyncSink{
				Uri: "oss://$EXTERNAL_FROM_ACCESS_KEY:$EXTERNAL_FROM_SECRET_KEY@example.com",
				Envs: []corev1.EnvVar{
					{
						Name: "EXTERNAL_FROM_ACCESS_KEY",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: common.GenSyncSecretName("sync1"),
								},
								Key: "EXTERNAL_FROM_ACCESS_KEY",
							},
						},
					},
					{
						Name: "EXTERNAL_FROM_SECRET_KEY",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "other_secret",
								},
								Key: "secretKey",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "JuiceFS sink",
			sink: juicefsiov1.SyncSink{
				JuiceFS: &juicefsiov1.SyncSinkJuiceFS{
					VolumeName: "volume",
					Path:       "path",
					Token: juicefsiov1.SyncSinkValue{
						Value: "token",
					},
					AuthOptions: []string{"option1", "--option2"},
					ConsoleUrl:  "http://127.0.0.1:8080/static",
				},
			},
			syncName: "sync2",
			ref:      "TO",
			want: &juicefsiov1.ParsedSyncSink{
				Uri: "jfs://volume/path",
				Envs: []corev1.EnvVar{
					{
						Name: "JUICEFS_TO_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: common.GenSyncSecretName("sync2"),
								},
								Key: "JUICEFS_TO_TOKEN",
							},
						},
					},
					{
						Name:  "BASE_URL",
						Value: "http://127.0.0.1:8080/static",
					},
				},
				PrepareCommand: "juicefs auth volume --token $JUICEFS_TO_TOKEN --option1 --option2",
			},
			wantErr: false,
		},
		{
			name: "from JuiceFS sink with files",
			sink: juicefsiov1.SyncSink{
				JuiceFS: &juicefsiov1.SyncSinkJuiceFS{
					VolumeName: "volume",
					Path:       "path",
					Token: juicefsiov1.SyncSinkValue{
						Value: "token",
					},
					FilesFrom: &juicefsiov1.SyncFilesFrom{
						Files: []string{"file1", "file2"},
					},
				},
			},
			syncName: "sync2",
			ref:      "FROM",
			want: &juicefsiov1.ParsedSyncSink{
				Uri: "jfs://volume/path",
				Envs: []corev1.EnvVar{
					{
						Name: "JUICEFS_FROM_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: common.GenSyncSecretName("sync2"),
								},
								Key: "JUICEFS_FROM_TOKEN",
							},
						},
					},
				},
				FilesFrom: &juicefsiov1.SyncFilesFrom{
					Files: []string{"file1", "file2"},
				},
				PrepareCommand: "juicefs auth volume --token $JUICEFS_FROM_TOKEN\nmkdir /tmp/sync-file\necho 'file1\nfile2' > /tmp/sync-file/files",
			},
			wantErr: false,
		},
		{
			name: "from JuiceFS sink with files from",
			sink: juicefsiov1.SyncSink{
				JuiceFS: &juicefsiov1.SyncSinkJuiceFS{
					VolumeName: "volume",
					Path:       "path",
					Token: juicefsiov1.SyncSinkValue{
						Value: "token",
					},
					FilesFrom: &juicefsiov1.SyncFilesFrom{
						ConfigMap: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "configmap",
							},
							Key: "file1",
						},
					},
				},
			},
			syncName: "sync2",
			ref:      "FROM",
			want: &juicefsiov1.ParsedSyncSink{
				Uri: "jfs://volume/path",
				Envs: []corev1.EnvVar{
					{
						Name: "JUICEFS_FROM_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: common.GenSyncSecretName("sync2"),
								},
								Key: "JUICEFS_FROM_TOKEN",
							},
						},
					},
				},
				FilesFrom: &juicefsiov1.SyncFilesFrom{
					ConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "configmap",
						},
						Key: "file1",
					},
				},
				PrepareCommand: "juicefs auth volume --token $JUICEFS_FROM_TOKEN",
			},
			wantErr: false,
		},
		{
			name: "to JuiceFS sink with files should fail",
			sink: juicefsiov1.SyncSink{
				JuiceFS: &juicefsiov1.SyncSinkJuiceFS{
					VolumeName: "volume",
					Path:       "path",
					Token: juicefsiov1.SyncSinkValue{
						Value: "token",
					},
					FilesFrom: &juicefsiov1.SyncFilesFrom{
						Files: []string{"file1", "file2"},
					},
					AuthOptions: []string{"option1", "--option2"},
					ConsoleUrl:  "http://127.0.0.1:8080/static",
				},
			},
			syncName: "sync2",
			ref:      "TO",
			want:     nil,
			wantErr:  true,
		},
		{
			name: "from JuiceFS sink with files from path",
			sink: juicefsiov1.SyncSink{
				JuiceFS: &juicefsiov1.SyncSinkJuiceFS{
					VolumeName: "volume",
					Token: juicefsiov1.SyncSinkValue{
						Value: "token",
					},
					FilesFrom: &juicefsiov1.SyncFilesFrom{
						FilePath: "/path/to/files",
					},
				},
			},
			syncName: "sync3",
			ref:      "FROM",
			want: &juicefsiov1.ParsedSyncSink{
				Uri: "jfs://volume",
				Envs: []corev1.EnvVar{
					{
						Name: "JUICEFS_FROM_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: common.GenSyncSecretName("sync3"),
								},
								Key: "JUICEFS_FROM_TOKEN",
							},
						},
					},
				},
				FilesFrom: &juicefsiov1.SyncFilesFrom{
					FilePath: "/path/to/files",
				},
				PrepareCommand: "juicefs auth volume --token $JUICEFS_FROM_TOKEN\nmkdir -p /tmp/sync-file && juicefs sync jfs://volume/path/to/files /tmp/sync-file/files",
			},
			wantErr: false,
		},
		{
			name:     "Invalid sink",
			sink:     juicefsiov1.SyncSink{},
			syncName: "sync3",
			ref:      "FROM",
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSyncSink(tt.sink, tt.syncName, tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSyncSink() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSyncSink() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSyncMetrics(t *testing.T) {
	metrics := `
# HELP juicefs_sync_copied Copied objects
# TYPE juicefs_sync_copied counter
juicefs_sync_copied{cmd="sync",pid="31287"} 1982
# HELP juicefs_sync_copied_bytes Copied bytes
# TYPE juicefs_sync_copied_bytes counter
juicefs_sync_copied_bytes{cmd="sync",pid="31287"} 1.768072e+06
# HELP juicefs_sync_handled Handled objects
# TYPE juicefs_sync_handled counter
juicefs_sync_handled{cmd="sync",pid="31287"} 1983
# HELP juicefs_sync_pending Pending objects
# TYPE juicefs_sync_pending gauge
juicefs_sync_pending{cmd="sync",pid="31287"} 10240
# HELP juicefs_sync_scanned Scanned objects
# TYPE juicefs_sync_scanned counter
juicefs_sync_scanned{cmd="sync",pid="31287"} 12282
# HELP juicefs_sync_skipped Skipped objects
# TYPE juicefs_sync_skipped counter
juicefs_sync_skipped{cmd="sync",pid="31287"} 1
# HELP juicefs_sync_skipped_bytes Skipped bytes
# TYPE juicefs_sync_skipped_bytes counter
juicefs_sync_skipped_bytes{cmd="sync",pid="31287"} 1.312189e+06
juicefs_sync_uptime 11.544506791
`
	metricsMap, err := ParseSyncMetrics(metrics)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]float64{
		"juicefs_sync_copied":        1982,
		"juicefs_sync_copied_bytes":  1768072,
		"juicefs_sync_handled":       1983,
		"juicefs_sync_pending":       10240,
		"juicefs_sync_scanned":       12282,
		"juicefs_sync_skipped":       1,
		"juicefs_sync_skipped_bytes": 1312189,
		"juicefs_sync_uptime":        11.544506791,
	}
	assert.Equal(t, expected, metricsMap)
}

func TestParseLog(t *testing.T) {
	data := "2025/02/28 07:36:12.271281 juicefs[148] <INFO>: Found: 4, skipped: 0 (0 B), copied: 4 (152.46 MiB), failed: 0 [sync.go:1387]"
	result, err := ParseLog(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{
		"found":        4,
		"copied":       4,
		"failed":       0,
		"skipped":      0,
		"copied_bytes": 159865896,
	}

	data2 := `
2025/03/14 14:34:18.094621 juicefs[78113] <DEBUG>: Try 3 failed: directory not empty [sync.go:206]
2025/03/14 14:34:22.095956 juicefs[78113] <ERROR>: Failed to copy data of  in 5.557595916s: directory not empty [sync.go:630]
2025/03/14 14:34:22.096341 juicefs[78113] <ERROR>: Failed to copy object : directory not empty [sync.go:718]
2025/03/14 14:34:22.112166 juicefs[78113] <INFO>: Found: 4, skipped: 0 (0 B), copied: 4 (152.46 MiB), failed: 0 [sync.go:1387]
2025/03/14 14:34:22.113225 juicefs[78113] <FATAL>: failed to handle 1 objects [main.go:68]
	`
	result2, err := ParseLog(data2)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, expected, result)
	assert.Equal(t, expected, result2)

	data3 := `error`
	_, err = ParseLog(data3)
	if err == nil {
		t.Fatal("ParseLog() should return error")
	}
}

func TestCalculateProgress(t *testing.T) {
	tests := []struct {
		name string
		a    int64
		b    int64
		want string
	}{
		{
			name: "Zero denominator",
			a:    1,
			b:    0,
			want: "0.00%",
		},
		{
			name: "Zero numerator",
			a:    0,
			b:    100,
			want: "0.00%",
		},
		{
			name: "Half progress",
			a:    3333,
			b:    10000,
			want: "33.33%",
		},
		{
			name: "Large numbers",
			a:    9811548006 - 1,
			b:    9811548006,
			want: "99.99%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateProgress(tt.a, tt.b); got != tt.want {
				t.Errorf("CalculateProgress() = %v, want %v", got, tt.want)
			}
		})
	}
}
