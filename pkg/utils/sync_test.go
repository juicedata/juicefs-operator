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

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
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
				Auth: "juicefs auth volume --token $JUICEFS_TO_TOKEN --option1 --option2",
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
