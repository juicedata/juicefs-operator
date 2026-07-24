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

func TestPodBuilder_genCacheDirs_VolumeDevice(t *testing.T) {
	tests := []struct {
		name                  string
		node                  string
		cacheDir              juicefsiov1.CacheDir
		expectedVolumes       []corev1.Volume
		expectedVolumeDevices []corev1.VolumeDevice
		expectedCommands      []string
	}{
		{
			name: "PVC",
			cacheDir: juicefsiov1.CacheDir{
				Type:       juicefsiov1.CacheDirTypePVC,
				Name:       "cache-pvc",
				VolumeMode: corev1.PersistentVolumeBlock,
			},
			expectedVolumes: []corev1.Volume{{
				Name: "jfs-cache-dir-0",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "cache-pvc",
					},
				},
			}},
			expectedVolumeDevices: []corev1.VolumeDevice{{
				Name:       "jfs-cache-dir-0",
				DevicePath: "/dev/jfs-cache-dir-0",
			}},
			expectedCommands: []string{
				"sh",
				"-c",
				`/usr/bin/juicefs auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}
CACHE_DEVICE=/dev/jfs-cache-dir-0
CACHE_DIR=/var/jfsCache-0
FORMAT_DEVICE=false

mkdir -p "$CACHE_DIR" || exit 1
blkid "$CACHE_DEVICE" >/dev/null 2>&1
case $? in
	0)
		;;
	2)
		if [ "$FORMAT_DEVICE" != "true" ]; then
			echo "Cache device $CACHE_DEVICE does not contain a recognized filesystem; set cacheDirs[].format to true to format it" >&2
			exit 1
		fi
		mkfs.ext4 -F "$CACHE_DEVICE" || exit 1
		;;
	*)
		exit 1
		;;
esac

mount "$CACHE_DEVICE" "$CACHE_DIR" || exit 1
exec /sbin/mount.juicefs test-name /mnt/jfs -o foreground,no-update,cache-group=default-test-cg,cache-dir=/var/jfsCache-0`,
			},
		},
		{
			name: "VolumeClaimTemplate with format",
			node: "node-1",
			cacheDir: juicefsiov1.CacheDir{
				Type:   juicefsiov1.CacheDirTypeVolumeClaimTemplates,
				Format: true,
				VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "cache-template"},
					Spec: corev1.PersistentVolumeClaimSpec{
						VolumeMode: utils.ToPtr(corev1.PersistentVolumeBlock),
					},
				},
			},
			expectedVolumes: []corev1.Volume{{
				Name: "jfs-cache-dir-0",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "cache-template-juicefs-cg-worker-test-cg-node-1",
					},
				},
			}},
			expectedVolumeDevices: []corev1.VolumeDevice{{
				Name:       "jfs-cache-dir-0",
				DevicePath: "/dev/jfs-cache-dir-0",
			}},
			expectedCommands: []string{
				"sh",
				"-c",
				`/usr/bin/juicefs auth test-name --token ${TOKEN} --secret-key ${SECRET_KEY}
CACHE_DEVICE=/dev/jfs-cache-dir-0
CACHE_DIR=/var/jfsCache-0
FORMAT_DEVICE=true

mkdir -p "$CACHE_DIR" || exit 1
blkid "$CACHE_DEVICE" >/dev/null 2>&1
case $? in
	0)
		;;
	2)
		if [ "$FORMAT_DEVICE" != "true" ]; then
			echo "Cache device $CACHE_DEVICE does not contain a recognized filesystem; set cacheDirs[].format to true to format it" >&2
			exit 1
		fi
		mkfs.ext4 -F "$CACHE_DEVICE" || exit 1
		;;
	*)
		exit 1
		;;
esac

mount "$CACHE_DEVICE" "$CACHE_DIR" || exit 1
exec /sbin/mount.juicefs test-name /mnt/jfs -o foreground,no-update,cache-group=default-test-cg,cache-dir=/var/jfsCache-0`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podBuilder := &PodBuilder{
				volName: "test-name",
				node:    tt.node,
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
					CacheDirs: []juicefsiov1.CacheDir{tt.cacheDir},
				},
			}

			podBuilder.genCacheDirs()

			if len(podBuilder.spec.VolumeMounts) != 0 {
				t.Errorf("VolumeMounts = %v, want none", podBuilder.spec.VolumeMounts)
			}

			if !reflect.DeepEqual(podBuilder.spec.Volumes, tt.expectedVolumes) {
				t.Errorf("Volumes = %v, want %v", podBuilder.spec.Volumes, tt.expectedVolumes)
			}

			if !reflect.DeepEqual(podBuilder.spec.VolumeDevices, tt.expectedVolumeDevices) {
				t.Errorf("VolumeDevices = %v, want %v", podBuilder.spec.VolumeDevices, tt.expectedVolumeDevices)
			}

			if got := podBuilder.genCommands(context.TODO()); !reflect.DeepEqual(got, tt.expectedCommands) {
				t.Errorf("genCommands() = %v, want %v", got, tt.expectedCommands)
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

func TestParseSecretConfigs(t *testing.T) {
	tests := []struct {
		name       string
		secretData map[string]string
		expected   map[string]string
	}{
		{
			name:       "missing configs",
			secretData: map[string]string{},
		},
		{
			name: "invalid configs",
			secretData: map[string]string{
				"configs": "-",
			},
		},
		{
			name: "valid configs",
			secretData: map[string]string{
				"configs": `{" juicefs-ca-cert ":" /root/.juicefs/juicefs-ca-cert ","relative":"tmp/config","empty":"","number":1}`,
			},
			expected: map[string]string{
				"juicefs-ca-cert": "/root/.juicefs/juicefs-ca-cert",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSecretConfigs(tt.secretData)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseSecretConfigs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAppendSecretConfigVolumes(t *testing.T) {
	volumes := []corev1.Volume{
		{
			Name: "jfs-config-1",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "existing-secret",
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "jfs-config-1",
			MountPath: "/existing",
		},
	}
	secretData := map[string]string{
		"configs": `{"secret-b":"/config/b","secret-a":"/config/a","secret-skip":"/existing"}`,
	}

	volumes, volumeMounts = appendSecretConfigVolumes(volumes, volumeMounts, secretData)

	expectedVolumes := []corev1.Volume{
		{
			Name: "jfs-config-1",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "existing-secret",
				},
			},
		},
		{
			Name: "jfs-config-2",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret-a",
				},
			},
		},
		{
			Name: "jfs-config-3",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret-b",
				},
			},
		},
	}
	expectedVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "jfs-config-1",
			MountPath: "/existing",
		},
		{
			Name:      "jfs-config-2",
			MountPath: "/config/a",
		},
		{
			Name:      "jfs-config-3",
			MountPath: "/config/b",
		},
	}

	if !reflect.DeepEqual(volumes, expectedVolumes) {
		t.Errorf("appendSecretConfigVolumes() volumes = %v, want %v", volumes, expectedVolumes)
	}
	if !reflect.DeepEqual(volumeMounts, expectedVolumeMounts) {
		t.Errorf("appendSecretConfigVolumes() volumeMounts = %v, want %v", volumeMounts, expectedVolumeMounts)
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
