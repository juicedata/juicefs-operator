package common

import "fmt"

const (
	// CacheGroupContainerName is the name of cache group worker container
	WorkerContainerName = "juicefs-cg-worker"
	// WorkerNamePrefix is the prefix of worker name
	WorkerNamePrefix = "juicefs-cg-worker"
	// Finalizer is the finalizer for CacheGroup
	Finalizer = "juicefs.io/finalizer"
	// juicefs binary path
	JuiceFSBinary = "/usr/bin/juicefs"
	MountPoint    = "/mnt/jfs"

	// label keys
	LabelCacheGroup = "juicefs.io/cache-group"
	LabelWorker     = "juicefs.io/worker"
	LabelWorkerHash = "juicefs.io/worker-hash"
)

func GenWorkerName(cgName string, nodeName string) string {
	return fmt.Sprintf("%s-%s-%s", WorkerNamePrefix, cgName, nodeName)
}
