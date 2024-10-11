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

package common

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// CacheGroupContainerName is the name of cache group worker container
	WorkerContainerName = "juicefs-cg-worker"
	// WorkerNamePrefix is the prefix of worker name
	WorkerNamePrefix = "juicefs-cg-worker"
	// Finalizer is the finalizer for CacheGroup
	Finalizer = "juicefs.io/finalizer"
	// juicefs binary path
	JuiceFSBinary      = "/usr/bin/juicefs"
	JuiceFsMountBinary = "/sbin/mount.juicefs"
	MountPoint         = "/mnt/jfs"

	// label keys
	LabelCacheGroup = "juicefs.io/cache-group"
	LabelWorker     = "juicefs.io/worker"
	LabelWorkerHash = "juicefs.io/worker-hash"
)

var (
	DefaultResources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
)

func GenWorkerName(cgName string, nodeName string) string {
	return fmt.Sprintf("%s-%s-%s", WorkerNamePrefix, cgName, nodeName)
}
