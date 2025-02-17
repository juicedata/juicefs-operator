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
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
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
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,no-update,cache-group=default-test-cg,cache-dir=/var/jfsCache",
			},
		},
		{
			name: "with cache-dir",
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
					CacheDirs: []juicefsiov1.CacheDir{
						{
							Type: juicefsiov1.CacheDirTypeHostPath,
							Path: "/custom/cache",
						},
					},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,no-update,cache-group=default-test-cg,cache-dir=/var/jfsCache-0",
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
					Opts: []string{"a=b", "verbose"},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,no-update,cache-group=default-test-cg,a=b,verbose,cache-dir=/var/jfsCache",
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
					Opts: []string{"verbose"},
				},
			},
			expected: []string{
				"sh",
				"-c",
				common.JuiceFSBinary + " auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY} --format-options --format-options2\n" +
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,no-update,cache-group=default-test-cg,verbose,cache-dir=/var/jfsCache",
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
					Spec: juicefsiov1.CacheGroupSpec{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
						},
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
					"exec " + common.JuiceFsMountBinary + " test-name " + common.MountPoint + " -o foreground,no-update,cache-group=default-test-cg,cache-dir=/var/jfsCache",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			tt.podBuilder.genInitConfigVolumes()
			tt.podBuilder.genCacheDirs()
			got := tt.podBuilder.genCommands(ctx)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("genCommands() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUpdateWorkerGroupWeight(t *testing.T) {
	tests := []struct {
		name     string
		worker   *corev1.Pod
		weight   int
		expected string
	}{
		{
			name: "no group-weight option",
			worker: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Command: []string{"sh", "-c", "cp /etc/juicefs/zxh-test-2.conf /root/.juicefs\nexec /sbin/mount.juicefs zxh-test-2 /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-cachegroup-sample,cache-dir=/var/jfsCache"},
					}},
				},
			},
			weight:   10,
			expected: "cp /etc/juicefs/zxh-test-2.conf /root/.juicefs\nexec /sbin/mount.juicefs zxh-test-2 /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-cachegroup-sample,cache-dir=/var/jfsCache,group-weight=10",
		},
		{
			name: "with group-weight option",
			worker: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Command: []string{"sh", "-c", "cp /etc/juicefs/zxh-test-2.conf /root/.juicefs\nexec /sbin/mount.juicefs zxh-test-2 /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-cachegroup-sample,cache-dir=/var/jfsCache,group-weight=10"},
					}},
				},
			},
			weight:   0,
			expected: "cp /etc/juicefs/zxh-test-2.conf /root/.juicefs\nexec /sbin/mount.juicefs zxh-test-2 /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-cachegroup-sample,cache-dir=/var/jfsCache,group-weight=0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateWorkerGroupWeight(tt.worker, tt.weight)
			if tt.worker.Spec.Containers[0].Command[2] != tt.expected {
				t.Errorf("UpdateWorkerGroupWeight() = %v, want %v", tt.worker.Spec.Containers[0].Command[2], tt.expected)
			}
		})
	}
}

func TestPodBuilder_genEnvs(t *testing.T) {
	tests := []struct {
		name       string
		podBuilder *PodBuilder
		expected   []corev1.EnvVar
	}{
		{
			name: "basic envs",
			podBuilder: &PodBuilder{
				cg: &juicefsiov1.CacheGroup{
					Spec: juicefsiov1.CacheGroupSpec{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
						},
					},
				},
				secretData: map[string]string{
					"envs": `{"ENV1": "value1", "ENV2": "value2"}`,
				},
			},
			expected: []corev1.EnvVar{
				{Name: "ENV1", Value: "value1"},
				{Name: "ENV2", Value: "value2"},
				{Name: "SECRET_KEY", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "secret-key",
						Optional: utils.ToPtr(true),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
				{Name: "SECRET_KEY_2", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "secret-key2",
						Optional: utils.ToPtr(true),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
				{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "token",
						Optional: utils.ToPtr(false),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
			},
		},
		{
			name: "no envs in secretData",
			podBuilder: &PodBuilder{
				cg: &juicefsiov1.CacheGroup{
					Spec: juicefsiov1.CacheGroupSpec{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
						},
					},
				},
				secretData: map[string]string{},
				spec:       juicefsiov1.CacheGroupWorkerTemplate{},
			},
			expected: []corev1.EnvVar{
				{Name: "SECRET_KEY", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "secret-key",
						Optional: utils.ToPtr(true),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
				{Name: "SECRET_KEY_2", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "secret-key2",
						Optional: utils.ToPtr(true),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
				{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      "token",
						Optional: utils.ToPtr(false),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			got := tt.podBuilder.genEnvs(ctx)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("genEnvs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseWorkerMountCmds(t *testing.T) {
	tests := []struct {
		name            string
		cmds            string
		expectedVolName string
		expectedOptions []string
	}{
		{
			name:            "valid command with options",
			cmds:            "exec /sbin/mount.juicefs test-vol /mnt/jfs -o option1,option2",
			expectedVolName: "test-vol",
			expectedOptions: []string{"option1", "option2"},
		},
		{
			name:            "valid command without options",
			cmds:            "exec /sbin/mount.juicefs test-vol /mnt/jfs -o ",
			expectedVolName: "test-vol",
			expectedOptions: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volName, options := utils.MustParseWorkerMountCmds(tt.cmds)
			if volName != tt.expectedVolName {
				t.Errorf("ParseMountCmds() volName = %v, want %v", volName, tt.expectedVolName)
			}
			if !reflect.DeepEqual(options, tt.expectedOptions) {
				t.Errorf("ParseMountCmds() options = %v, want %v", options, tt.expectedOptions)
			}
		})
	}
}
