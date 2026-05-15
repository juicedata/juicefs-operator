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

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
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

func TestPodBuilder_genCacheDirs_HostPathType(t *testing.T) {
	tests := []struct {
		name             string
		cacheDirs        []juicefsiov1.CacheDir
		expectedPathType *corev1.HostPathType
	}{
		{
			name: "default hostPathType (nil)",
			cacheDirs: []juicefsiov1.CacheDir{
				{
					Type: juicefsiov1.CacheDirTypeHostPath,
					Path: "/mnt/cache",
				},
			},
			expectedPathType: utils.ToPtr(corev1.HostPathDirectoryOrCreate),
		},
		{
			name: "explicit DirectoryOrCreate",
			cacheDirs: []juicefsiov1.CacheDir{
				{
					Type:         juicefsiov1.CacheDirTypeHostPath,
					Path:         "/mnt/cache",
					HostPathType: utils.ToPtr(corev1.HostPathDirectoryOrCreate),
				},
			},
			expectedPathType: utils.ToPtr(corev1.HostPathDirectoryOrCreate),
		},
		{
			name: "Directory type",
			cacheDirs: []juicefsiov1.CacheDir{
				{
					Type:         juicefsiov1.CacheDirTypeHostPath,
					Path:         "/mnt/cache",
					HostPathType: utils.ToPtr(corev1.HostPathDirectory),
				},
			},
			expectedPathType: utils.ToPtr(corev1.HostPathDirectory),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podBuilder := &PodBuilder{
				spec: juicefsiov1.CacheGroupWorkerTemplate{
					CacheDirs: tt.cacheDirs,
				},
			}

			podBuilder.genCacheDirs()

			// Verify the volume was created with correct HostPathType
			if len(podBuilder.spec.Volumes) != 1 {
				t.Fatalf("Expected 1 volume, got %d", len(podBuilder.spec.Volumes))
			}

			volume := podBuilder.spec.Volumes[0]
			if volume.VolumeSource.HostPath == nil {
				t.Fatal("Expected HostPath volume source, got nil")
			}

			if volume.VolumeSource.HostPath.Type == nil {
				t.Fatal("Expected HostPath Type to be set, got nil")
			}

			if *volume.VolumeSource.HostPath.Type != *tt.expectedPathType {
				t.Errorf("Expected HostPathType %v, got %v", *tt.expectedPathType, *volume.VolumeSource.HostPath.Type)
			}
		})
	}
}

func TestPodBuilder_NewCacheGroupWorker_EnableServiceLinks(t *testing.T) {
	makeCG := func() *juicefsiov1.CacheGroup {
		return &juicefsiov1.CacheGroup{
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
		}
	}
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"name":  []byte("test-vol"),
			"token": []byte("test-token"),
		},
	}

	t.Run("EnableServiceLinks not set (nil)", func(t *testing.T) {
		pb := NewPodBuilder(makeCG(), secret, "node1", juicefsiov1.CacheGroupWorkerTemplate{}, false)
		worker := pb.NewCacheGroupWorker(context.TODO(), false)
		if worker.Spec.EnableServiceLinks != nil {
			t.Errorf("expected EnableServiceLinks to be nil, got %v", worker.Spec.EnableServiceLinks)
		}
	})

	t.Run("EnableServiceLinks set to false", func(t *testing.T) {
		spec := juicefsiov1.CacheGroupWorkerTemplate{
			EnableServiceLinks: utils.ToPtr(false),
		}
		pb := NewPodBuilder(makeCG(), secret, "node1", spec, false)
		worker := pb.NewCacheGroupWorker(context.TODO(), false)
		if worker.Spec.EnableServiceLinks == nil {
			t.Fatal("expected EnableServiceLinks to be set, got nil")
		}
		if *worker.Spec.EnableServiceLinks != false {
			t.Errorf("expected EnableServiceLinks=false, got %v", *worker.Spec.EnableServiceLinks)
		}
	})

	t.Run("EnableServiceLinks set to true", func(t *testing.T) {
		spec := juicefsiov1.CacheGroupWorkerTemplate{
			EnableServiceLinks: utils.ToPtr(true),
		}
		pb := NewPodBuilder(makeCG(), secret, "node1", spec, false)
		worker := pb.NewCacheGroupWorker(context.TODO(), false)
		if worker.Spec.EnableServiceLinks == nil {
			t.Fatal("expected EnableServiceLinks to be set, got nil")
		}
		if *worker.Spec.EnableServiceLinks != true {
			t.Errorf("expected EnableServiceLinks=true, got %v", *worker.Spec.EnableServiceLinks)
		}
	})
}

func TestMergeCacheGroupWorkerTemplate_EnableServiceLinks(t *testing.T) {
	t.Run("overwrite EnableServiceLinks", func(t *testing.T) {
		template := &juicefsiov1.CacheGroupWorkerTemplate{}
		overwrite := juicefsiov1.CacheGroupWorkerOverwrite{
			CacheGroupWorkerTemplate: juicefsiov1.CacheGroupWorkerTemplate{
				EnableServiceLinks: utils.ToPtr(false),
			},
		}
		MergeCacheGroupWorkerTemplate(template, overwrite)
		if template.EnableServiceLinks == nil {
			t.Fatal("expected EnableServiceLinks to be set after merge")
		}
		if *template.EnableServiceLinks != false {
			t.Errorf("expected EnableServiceLinks=false, got %v", *template.EnableServiceLinks)
		}
	})

	t.Run("overwrite nil does not change template", func(t *testing.T) {
		template := &juicefsiov1.CacheGroupWorkerTemplate{
			EnableServiceLinks: utils.ToPtr(true),
		}
		overwrite := juicefsiov1.CacheGroupWorkerOverwrite{}
		MergeCacheGroupWorkerTemplate(template, overwrite)
		if template.EnableServiceLinks == nil || !*template.EnableServiceLinks {
			t.Errorf("expected EnableServiceLinks to remain true")
		}
	})
}

func TestPodBuilder_genCacheDirs_PVCIgnoresHostPathType(t *testing.T) {
	podBuilder := &PodBuilder{
		spec: juicefsiov1.CacheGroupWorkerTemplate{
			CacheDirs: []juicefsiov1.CacheDir{
				{
					Type:         juicefsiov1.CacheDirTypePVC,
					Name:         "my-pvc",
					HostPathType: utils.ToPtr(corev1.HostPathDirectory), // Should be ignored
				},
			},
		},
	}

	podBuilder.genCacheDirs()

	// Verify PVC volume was created (not HostPath)
	if len(podBuilder.spec.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(podBuilder.spec.Volumes))
	}

	volume := podBuilder.spec.Volumes[0]
	if volume.VolumeSource.PersistentVolumeClaim == nil {
		t.Fatal("Expected PVC volume source, got nil")
	}

	if volume.VolumeSource.HostPath != nil {
		t.Fatal("Expected no HostPath volume source for PVC type")
	}
}
