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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	GroupVersion   = "juicefs.io/v1"
	KindCaCheGroup = "CacheGroup"
	KindSync       = "Sync"
	KindWarmUp     = "WarmUp"

	// CacheGroupContainerName is the name of cache group worker container
	WorkerContainerName     = "juicefs-cg-worker"
	WarmUpContainerName     = "juicefs-warmup"
	CleanCacheContainerName = "juicefs-clean-cache"
	// WorkerNamePrefix is the prefix of worker name
	WorkerNamePrefix = "juicefs-cg-worker"
	WarmUpNamePrefix = "juicefs-warmup"
	SyncNamePrefix   = "juicefs-sync"
	// Finalizer is the finalizer for CacheGroup
	Finalizer = "juicefs.io/finalizer"
	// juicefs binary path
	JuiceFSBinary                 = "/usr/bin/juicefs"
	JuiceFsMountBinary            = "/sbin/mount.juicefs"
	MountPoint                    = "/mnt/jfs"
	CacheDirVolumeNamePrefix      = "jfs-cache-dir-"
	CacheDirVolumeMountPathPrefix = "/var/jfsCache-"
	DefaultCacheHostPath          = "/var/jfsCache"
	SyncFileFromPath              = "/tmp/sync-file"
	SyncFileFromName              = "files"

	// label keys
	LabelManagedBy          = "app.kubernetes.io/managed-by"
	LabelManagedByValue     = "juicefs-operator"
	LabelCacheGroup         = "juicefs.io/cache-group"
	LabelWorkerHash         = "juicefs.io/worker-hash"
	LabelAppType            = "app.kubernetes.io/name"
	LabelWorkerValue        = "juicefs-cache-group-worker"
	LabelJobValue           = "juicefs-warmup-job"
	LabelCronJobValue       = "juicefs-warmup-cron-job"
	LabelCleanCacheJobValue = "juicefs-clean-cache-job"
	LabelSync               = "juicefs.io/sync"
	LabelSyncWorkerValue    = "juicefs-sync-worker"
	LabelSyncManagerValue   = "juicefs-sync-manager"
	LabelCronSync           = "juicefs.io/cron-sync"

	AnnoBackupWorker        = "juicefs.io/backup-worker"
	AnnoWaitingDeleteWorker = "juicefs.io/waiting-delete-worker"

	LabelMaxLength       = 63
	CronJobNameMaxLength = 52

	InitConfigVolumeName = "init-config"
	InitConfigVolumeKey  = "initconfig"
	InitConfigMountPath  = "/etc/juicefs"

	MinSupportedWarmupStatsVersion = "5.2.11"
)

var (
	DefaultResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
	}

	DefaultForCleanCacheResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}

	DefaultBackupWorkerDuration = 10 * time.Minute
	DefaultWaitingMaxDuration   = 1 * time.Hour

	MaxSyncConcurrentReconciles   = 10
	MaxWarmupConcurrentReconciles = 10

	UpdateWarmupStatsInterval = 3 * time.Second

	K8sClientQPS   float32 = 30
	K8sClientBurst int     = 20

	OperatorPod          *corev1.Pod
	OperatorPodName      = ""
	OperatorPodNamespace = ""
)

func GenWorkerName(cgName string, nodeName string) string {
	return fmt.Sprintf("%s-%s-%s", WorkerNamePrefix, cgName, nodeName)
}

func GenCleanCacheJobName(cgName, nodeName string) string {
	return fmt.Sprintf("%s-%s-%s", CleanCacheContainerName, cgName, nodeName)
}

func GenSyncSecretName(syncJobName string) string {
	return fmt.Sprintf("juicefs-%s-secrets", syncJobName)
}

func GenSyncWorkerName(syncJobName string, i int) string {
	return fmt.Sprintf("%s-%s-worker-%d", SyncNamePrefix, syncJobName, i)
}

func GenSyncManagerName(syncJobName string) string {
	return fmt.Sprintf("%s-%s-manager", SyncNamePrefix, syncJobName)
}

func GenPVCName(templateName, nodeName string) string {
	return fmt.Sprintf("%s-%s", templateName, nodeName)
}
