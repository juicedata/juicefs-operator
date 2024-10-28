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

package builder

import (
	"context"
	"reflect"
	"testing"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodBuilder_genCommands(t *testing.T) {
	tests := []struct {
		name       string
		podBuilder *PodBuilder
		expected   []string
	}{
		{
			name: "basic commands",
			podBuilder: &PodBuilder{
				volName: "test-name",
				cg: &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cg",
						Namespace: "default",
					},
				},
				secretData: map[string]string{
					"token":      "test-token",
					"secret-key": "test-secret-key",
				},
				spec: juicefsiov1.CacheGroupWorkerTemplate{
					Opts: []string{},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,cache-dir=/var/jfsCache",
			},
		},
		{
			name: "with cache-dir option",
			podBuilder: &PodBuilder{
				volName: "test-name",
				cg: &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cg",
						Namespace: "default",
					},
				},
				secretData: map[string]string{
					"token":      "test-token",
					"secret-key": "test-secret-key",
				},
				spec: juicefsiov1.CacheGroupWorkerTemplate{
					Opts: []string{"cache-dir=/custom/cache"},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,cache-dir=/custom/cache",
			},
		},
		{
			name: "with multiple options",
			podBuilder: &PodBuilder{
				volName: "test-name",
				cg: &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cg",
						Namespace: "default",
					},
				},
				secretData: map[string]string{
					"token":      "test-token",
					"secret-key": "test-secret-key",
				},
				spec: juicefsiov1.CacheGroupWorkerTemplate{
					Opts: []string{"cache-dir=/custom/cache", "verbose"},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,verbose,cache-dir=/custom/cache",
			},
		},
		{
			name: "with format-options",
			podBuilder: &PodBuilder{
				volName: "test-name",
				cg: &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cg",
						Namespace: "default",
					},
				},
				secretData: map[string]string{
					"token":          "test-token",
					"secret-key":     "test-secret-key",
					"format-options": "format-options,format-options2",
				},
				spec: juicefsiov1.CacheGroupWorkerTemplate{
					Opts: []string{"cache-dir=/custom/cache", "verbose"},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY} --format-options --format-options2\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,verbose,cache-dir=/custom/cache",
			},
		},
		{
			name: "with initconfig",
			podBuilder: &PodBuilder{
				volName: "test-name",
				cg: &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cg",
						Namespace: "default",
					},
				},
				initConfig: "initconfig",
				secretData: map[string]string{
					"token":      "test-token",
					"secret-key": "test-secret-key",
					"initconfig": "initconfig",
				},
			},
			expected: []string{
				"sh",
				"-c",
				"cp /etc/juicefs/test-name.conf /root/.juicefs\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,cache-dir=/var/jfsCache",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			got := tt.podBuilder.genCommands(ctx)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("genCommands() = %v, want %v", got, tt.expected)
			}
		})
	}
}
