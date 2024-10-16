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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenAuthCmdWithSecret(t *testing.T) {
	tests := []struct {
		name     string
		secrets  map[string]string
		expected []string
	}{
		{
			name: "basic secrets",
			secrets: map[string]string{
				"name":       "test-name",
				"token":      "test-token",
				"secret-key": "test-secret-key",
			},
			expected: []string{
				common.JuiceFSBinary,
				"auth",
				"test-name",
				"--token", "${TOKEN}",
				"--secret-key", "${SECRET_KEY}",
			},
		},
		{
			name: "all secrets",
			secrets: map[string]string{
				"name":        "test-name",
				"token":       "test-token",
				"access-key":  "test-access-key",
				"access-key2": "test-access-key2",
				"bucket":      "test-bucket",
				"bucket2":     "test-bucket2",
				"subdir":      "test-subdir",
				"secret-key":  "test-secret-key",
				"secret-key2": "test-secret-key2",
			},
			expected: []string{
				common.JuiceFSBinary,
				"auth",
				"test-name",
				"--token", "${TOKEN}",
				"--access-key", "test-access-key",
				"--access-key2", "test-access-key2",
				"--bucket", "test-bucket",
				"--bucket2", "test-bucket2",
				"--subdir", "test-subdir",
				"--secret-key", "${SECRET_KEY}",
				"--secret-key2", "${SECRET_KEY_2}",
			},
		},
		{
			name: "with format-options",
			secrets: map[string]string{
				"name":           "test-name",
				"token":          "test-token",
				"format-options": "opt1=val1,opt2=val2,opt3",
			},
			expected: []string{
				common.JuiceFSBinary,
				"auth",
				"test-name",
				"--token", "${TOKEN}",
				"--opt1", "val1",
				"--opt2", "val2",
				"--opt3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			got := genAuthCmdWithSecret(ctx, tt.secrets)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("genAuthCmdWithSecret() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGenCommands(t *testing.T) {
	tests := []struct {
		name     string
		cg       *juicefsiov1.CacheGroup
		secrets  *corev1.Secret
		spec     juicefsiov1.CacheGroupWorkerTemplate
		expected []string
	}{
		{
			name: "basic commands",
			cg: &juicefsiov1.CacheGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cg",
					Namespace: "default",
				},
			},
			secrets: &corev1.Secret{
				Data: map[string][]byte{
					"name":       []byte("test-name"),
					"token":      []byte("test-token"),
					"secret-key": []byte("test-secret-key"),
				},
			},
			spec: juicefsiov1.CacheGroupWorkerTemplate{
				Opts: []string{},
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
			cg: &juicefsiov1.CacheGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cg",
					Namespace: "default",
				},
			},
			secrets: &corev1.Secret{
				Data: map[string][]byte{
					"name":       []byte("test-name"),
					"token":      []byte("test-token"),
					"secret-key": []byte("test-secret-key"),
				},
			},
			spec: juicefsiov1.CacheGroupWorkerTemplate{
				Opts: []string{"cache-dir=/custom/cache"},
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
			cg: &juicefsiov1.CacheGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cg",
					Namespace: "default",
				},
			},
			secrets: &corev1.Secret{
				Data: map[string][]byte{
					"name":       []byte("test-name"),
					"token":      []byte("test-token"),
					"secret-key": []byte("test-secret-key"),
				},
			},
			spec: juicefsiov1.CacheGroupWorkerTemplate{
				Opts: []string{"cache-dir=/custom/cache", "verbose"},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,cache-group=default-test-cg,verbose,cache-dir=/custom/cache",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			got := genCommands(ctx, tt.cg, tt.secrets, tt.spec)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("genCommands() = %v, want %v", got, tt.expected)
			}
		})
	}
}
