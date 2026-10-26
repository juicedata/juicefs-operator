/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/scheduler"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	"github.com/samber/lo"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// CacheGroupReconciler reconciles a CacheGroup object
type CacheGroupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;create;watch;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create;delete;get;list;update;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CacheGroup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *CacheGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	cg := &juicefsiov1.CacheGroup{}
	if err := r.Get(ctx, req.NamespacedName, cg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	l.V(1).Info("start reconciler")
	if cg.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(cg, common.Finalizer) {
			controllerutil.AddFinalizer(cg, common.Finalizer)
			return ctrl.Result{}, r.Update(ctx, cg)
		}
	} else {
		// The object is being deleted
		l.Info("CacheGroup is being deleted, start clean resources", "name", cg.Name)
		if controllerutil.ContainsFinalizer(cg, common.Finalizer) {
			if err := r.HandleFinalizer(ctx, cg); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(cg, common.Finalizer)
			if err := r.Update(ctx, cg); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
		}

		return ctrl.Result{}, nil
	}

	// compare the state specified by the CacheGroup object against the actual cluster state
	expectStates, err := r.parseExpectState(ctx, cg)
	if err != nil {
		l.Error(err, "failed to parse expect state")
		return ctrl.Result{}, err
	}

	if err := r.sync(ctx, cg, expectStates); err != nil {
		l.Error(err, "failed to sync cache group workers")
		return ctrl.Result{}, err
	}
	l.V(1).Info("reconciler done")
	if cg.Status.Rebalance != nil || cg.Status.BackUpWorker > 0 || cg.Status.WaitingDeletedWorker > 0 || cg.Status.Phase != juicefsiov1.CacheGroupPhaseReady {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Minute,
		}, nil
	}
	return ctrl.Result{}, nil
}

// sync synchronizes the cache group workers with the expect states
func (r *CacheGroupReconciler) sync(ctx context.Context, cg *juicefsiov1.CacheGroup, expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate) error {
	log := log.FromContext(ctx)
	// TODO: add a webook to validate the cache group worker template
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: cg.Spec.SecretRef.Name}, secret); err != nil {
		log.Error(err, "failed to get secret", "secret", cg.Spec.SecretRef.Name)
		return err
	}
	if err := utils.ValidateSecret(secret); err != nil {
		log.Error(err, "failed to validate secret")
		return err
	}

	actualWorkers, err := r.listActualWorkers(ctx, cg)
	if err != nil {
		log.Error(err, "failed to list actual worker nodes")
		return err
	}
	base := cg.DeepCopy()
	fileSystem := strings.TrimSpace(string(secret.Data["name"]))
	cg.Status = r.calculateStatus(ctx, cg, fileSystem, expectStates, actualWorkers)
	workerStats := lo.KeyBy(cg.Status.Stats, func(stats juicefsiov1.CacheGroupWorkerStats) string { return stats.Name })

	syncErr := r.syncWorkers(ctx, cg, secret, expectStates, actualWorkers, workerStats)
	errors := []error{syncErr}
	if syncErr == nil {
		errors = append(errors, r.removeRedundantWorkers(ctx, cg, expectStates, actualWorkers, workerStats))
	}

	actualWorkers, err = r.listActualWorkers(ctx, cg)
	if err != nil {
		log.Error(err, "failed to list actual worker nodes")
		return utilerrors.NewAggregate(append(errors, err))
	}

	cg.Status = r.calculateStatus(ctx, cg, fileSystem, expectStates, actualWorkers)
	if !reflect.DeepEqual(base.Status, cg.Status) {
		errors = append(errors, utils.IgnoreConflict(r.Status().Patch(ctx, cg, client.MergeFrom(base))))
	}
	return utilerrors.NewAggregate(errors)
}

func (r *CacheGroupReconciler) parseExpectState(ctx context.Context, cg *juicefsiov1.CacheGroup) (map[string]juicefsiov1.CacheGroupWorkerTemplate, error) {
	if cg.Spec.Replicas != nil {
		return r.parseExpectStateByReplicas(cg), nil
	}
	if cg.Spec.EnableScheduling {
		return r.parseExpectStateByScheduler(ctx, cg)
	}
	return r.parseExpectStateByNodeSelector(ctx, cg)
}

func (r *CacheGroupReconciler) parseExpectStateByReplicas(cg *juicefsiov1.CacheGroup) map[string]juicefsiov1.CacheGroupWorkerTemplate {
	expectStates := map[string]juicefsiov1.CacheGroupWorkerTemplate{}
	for i := 0; i < int(*cg.Spec.Replicas); i++ {
		workerName := fmt.Sprintf("%s-%s-%d", common.WorkerNamePrefix, cg.Name, i)
		expectStates[workerName] = *cg.Spec.Worker.Template.DeepCopy()
	}
	return expectStates
}

func (r *CacheGroupReconciler) parseExpectStateWithNode(cg *juicefsiov1.CacheGroup, nodes []corev1.Node) map[string]juicefsiov1.CacheGroupWorkerTemplate {
	expectStates := map[string]juicefsiov1.CacheGroupWorkerTemplate{}
	for _, node := range nodes {
		expectState := cg.Spec.Worker.Template.DeepCopy()
		for _, overwrite := range cg.Spec.Worker.Overwrite {
			if utils.SliceContains(overwrite.Nodes, node.Name) ||
				(overwrite.NodeSelector != nil && utils.NodeSelectorContains(overwrite.NodeSelector, node.Labels)) {
				builder.MergeCacheGroupWorkerTemplate(expectState, overwrite)
			}
		}
		expectStates[node.Name] = *expectState
	}
	return expectStates
}

func (r *CacheGroupReconciler) parseExpectStateByNodeSelector(ctx context.Context, cg *juicefsiov1.CacheGroup) (map[string]juicefsiov1.CacheGroupWorkerTemplate, error) {
	expectAppliedNodes := corev1.NodeList{}
	err := r.List(ctx, &expectAppliedNodes, client.MatchingLabels(cg.Spec.Worker.Template.NodeSelector))
	if err != nil {
		return nil, err
	}
	return r.parseExpectStateWithNode(cg, expectAppliedNodes.Items), nil
}

// parseExpectStateByScheduler
// make cg respect the affinity and tolerations of the scheduler
func (r *CacheGroupReconciler) parseExpectStateByScheduler(ctx context.Context, cg *juicefsiov1.CacheGroup) (map[string]juicefsiov1.CacheGroupWorkerTemplate, error) {
	selector := labels.Everything()
	if cg.Spec.Worker.Template.NodeSelector != nil {
		selector = labels.SelectorFromSet(cg.Spec.Worker.Template.NodeSelector)
	}
	checkNodes := corev1.NodeList{}
	err := r.List(ctx, &checkNodes, client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		return nil, err
	}
	if len(checkNodes.Items) == 0 {
		return nil, nil
	}
	sim := scheduler.NewSchedulerSimulator(r.Client, checkNodes.Items)
	expectStates := r.parseExpectStateWithNode(cg, checkNodes.Items)

	nodes := checkNodes.Items
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})
	for _, node := range nodes {
		state := expectStates[node.Name]
		podBuilder := builder.NewPodBuilder(cg, &corev1.Secret{}, node.Name, state, false)
		pod := podBuilder.NewCacheGroupWorker(ctx, true)
		if ok, err := sim.CanSchedulePodOnNode(ctx, pod, &node); err != nil {
			return nil, err
		} else {
			if !ok {
				delete(expectStates, node.Name)
				sim.AppendToBeDeletedPod(pod)
				continue
			}
			pod.Spec.NodeName = node.Name
			sim.AppendPendingPod(pod)
		}
	}
	return expectStates, nil
}

func (r *CacheGroupReconciler) getPodName(cg *juicefsiov1.CacheGroup, node string) string {
	if cg.Spec.Replicas != nil {
		return node
	} else {
		return common.GenWorkerName(cg.Name, node)
	}
}

func (r *CacheGroupReconciler) createOrUpdateWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, actual, expect *corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", expect.Name)
	if actual == nil {
		log.Info("create worker")
		return r.createCacheGroupWorker(ctx, cg, expect)
	}
	return r.updateCacheGroupWorker(ctx, cg, actual, expect)
}

// ensurePVCsForWorker ensures all PVCs for a worker are created and expanded when requested.
func (r *CacheGroupReconciler) ensurePVCsForWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string, spec juicefsiov1.CacheGroupWorkerTemplate) error {
	for i, cacheDir := range spec.CacheDirs {
		if cacheDir.Type == juicefsiov1.CacheDirTypeVolumeClaimTemplates {
			if err := r.ensurePVCForVolumeClaimTemplate(ctx, cg, workerName, cacheDir.VolumeClaimTemplate); err != nil {
				return fmt.Errorf("failed to ensure PVC for cache dir %d: %w", i, err)
			}
		}
	}
	return nil
}

func (r *CacheGroupReconciler) createCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, expectWorker *corev1.Pod) error {
	err := r.Create(ctx, expectWorker)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		r.eventf(cg, corev1.EventTypeWarning, "WorkerOperationFailed", "Failed to create worker %s: %v", expectWorker.Name, err)
		return err
	}
	r.eventf(cg, corev1.EventTypeNormal, "WorkerCreated", "Worker %s is created", expectWorker.Name)
	return nil
}

func (r *CacheGroupReconciler) updateCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, oldWorker, expectWorker *corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", expectWorker.Name)
	log.Info("worker spec changed, delete old and create new one")
	if err := r.deleteCacheGroupWorker(ctx, cg, oldWorker, true); err != nil {
		return err
	}
	if err := r.Create(ctx, expectWorker); err != nil {
		r.eventf(cg, corev1.EventTypeWarning, "WorkerOperationFailed", "Failed to recreate worker %s: %v", expectWorker.Name, err)
		return err
	}
	r.eventf(cg, corev1.EventTypeNormal, "WorkerUpdated", "Worker %s spec changed, recreated", expectWorker.Name)
	return nil
}

// deleteCacheGroupWorker deletes a cache group worker pod. If the `waiting` is true,
// it waits until the pod is confirmed to be deleted or a timeout occurs.
func (r *CacheGroupReconciler) deleteCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, worker *corev1.Pod, waiting bool) (err error) {
	log := log.FromContext(ctx).WithValues("worker", worker.Name)
	defer func() {
		if err != nil {
			log.Error(err, "failed to delete worker")
			r.eventf(cg, corev1.EventTypeWarning, "WorkerOperationFailed", "Failed to delete worker %s: %v", worker.Name, err)
		}
	}()
	err = r.Delete(ctx, worker)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	r.eventf(cg, corev1.EventTypeNormal, "WorkerDeleted", "Worker %s is deleted", worker.Name)
	if waiting {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		for {
			err := r.Get(ctx, client.ObjectKey{Namespace: worker.Namespace, Name: worker.Name}, worker)
			if apierrors.IsNotFound(err) {
				break
			}
			if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("timeout waiting for worker to be deleted")
			}
			time.Sleep(time.Second)
		}
	}
	return nil
}

func (r *CacheGroupReconciler) listActualWorkers(ctx context.Context, cg *juicefsiov1.CacheGroup) ([]corev1.Pod, error) {
	workers := corev1.PodList{}
	if err := r.List(ctx, &workers, client.InNamespace(cg.Namespace), client.MatchingLabels{
		common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
		common.LabelAppType:    common.LabelWorkerValue,
	}); err != nil {
		return nil, err
	}
	return workers.Items, nil
}

func (r *CacheGroupReconciler) calculateStatus(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	fileSystem string,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod) juicefsiov1.CacheGroupStatus {
	newStatus := cg.Status
	newStatus.FileSystem = fileSystem
	if len(expectStates) == 0 {
		newStatus.ReadyStr = "-"
		newStatus.ReadyWorker = 0
		newStatus.ExpectWorker = 0
		newStatus.BackUpWorker = 0
		newStatus.Rebalance = nil
		newStatus.Phase = juicefsiov1.CacheGroupPhaseWaiting
		return newStatus
	}
	backupWorker := 0
	waitingDeletedWorker := 0
	readyWorker := 0
	batch := cg.Status.Rebalance.DeepCopy()
	delay := utils.CacheGroupRebalanceDelay(actualWorks)
	for _, worker := range actualWorks {
		if joinedAt, ok := worker.Annotations[common.AnnoBackupWorker]; ok {
			backupWorker++
			if batch == nil {
				batch = &juicefsiov1.CacheGroupRebalanceStatus{Members: []string{}}
			}
			if !slices.Contains(batch.Members, worker.Name) {
				batch.Members = append(batch.Members, worker.Name)
				startAt := utils.MustParseTime(joinedAt).Add(delay)
				if startAt.After(batch.StartAt.Time) {
					batch.StartAt = metav1.NewTime(startAt)
				}
			}
		}
		if _, ok := worker.Annotations[common.AnnoWaitingDeleteWorker]; ok {
			waitingDeletedWorker++
		}
		if utils.IsPodReady(worker) {
			readyWorker++
		}
	}
	if batch != nil {
		expected := make(map[string]bool, len(expectStates))
		for node := range expectStates {
			expected[r.getPodName(cg, node)] = true
		}
		actual := lo.KeyBy(actualWorks, func(worker corev1.Pod) string { return worker.Name })
		batch.Members = slices.DeleteFunc(batch.Members, func(name string) bool {
			_, exists := actual[name]
			return !expected[name] && !exists
		})
		slices.Sort(batch.Members)
		finished := !time.Now().Before(batch.StartAt.Time)
		for _, name := range batch.Members {
			worker, exists := actual[name]
			_, joining := worker.Annotations[common.AnnoBackupWorker]
			if !exists || !expected[name] || joining || worker.DeletionTimestamp != nil || !utils.IsPodReady(worker) {
				finished = false
				break
			}
		}
		// Bound how long the batch blocks new workers, even if join annotations remain.
		deadline := batch.StartAt.Add(utils.GetBackupWorkerDuration(cg.Spec.BackupDuration))
		if len(batch.Members) == 0 || finished {
			batch = nil
		} else if !time.Now().Before(deadline) {
			log.FromContext(ctx).Info("rebalance batch timed out, stop waiting", "members", batch.Members, "deadline", deadline.Format(time.RFC3339))
			batch = nil
		}
	}
	newStatus.Rebalance = batch
	newStatus.ReadyWorker = int32(readyWorker)
	newStatus.ExpectWorker = int32(len(expectStates))
	newStatus.BackUpWorker = int32(backupWorker)
	newStatus.WaitingDeletedWorker = int32(waitingDeletedWorker)
	newStatus.ReadyStr = fmt.Sprintf("%d/%d", newStatus.ReadyWorker, newStatus.ExpectWorker)
	if newStatus.ExpectWorker != newStatus.ReadyWorker {
		newStatus.Phase = juicefsiov1.CacheGroupPhaseProgressing
	} else {
		newStatus.Phase = juicefsiov1.CacheGroupPhaseReady
	}
	newStatus.CacheGroup = builder.GenCacheGroupName(cg)
	return newStatus
}

func (r *CacheGroupReconciler) cleanWorkerCache(ctx context.Context, cg *juicefsiov1.CacheGroup, worker corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", worker.Name)
	if !cg.Spec.CleanCache {
		return nil
	}
	if cg.Spec.Replicas != nil {
		// Replica workers are scheduled dynamically, so only delete their PVCs.
		return r.deletePVCForCacheGroup(ctx, cg, worker)
	}

	// PVCs owned by the CacheGroup are deleted separately; external PVCs still need cache cleanup.
	cleanVolumes := make([]corev1.Volume, 0, len(worker.Spec.Volumes))
	for _, volume := range worker.Spec.Volumes {
		if !strings.HasPrefix(volume.Name, common.CacheDirVolumeNamePrefix) {
			continue
		}
		if volume.HostPath != nil {
			cleanVolumes = append(cleanVolumes, volume)
			continue
		}
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		pvc := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !metav1.IsControlledBy(pvc, cg) {
			cleanVolumes = append(cleanVolumes, volume)
		}
	}
	workerForCleanup := worker
	workerForCleanup.Spec.Volumes = cleanVolumes
	job := builder.NewCleanCacheJob(*cg, workerForCleanup)
	if job == nil {
		return r.deletePVCForCacheGroup(ctx, cg, worker)
	}
	log.Info("worker is to be deleted, create job to clean cache", "job", job.Name)
	err := r.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name}, &batchv1.Job{})
	if err == nil {
		log.Info("clean cache job already exists", "job", job.Name)
	} else {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get clean cache job", "job", job.Name)
			return err
		}
		if err := r.Create(ctx, job); err != nil {
			log.Error(err, "failed to create clean cache job", "job", job.Name)
			return err
		}
	}
	return r.deletePVCForCacheGroup(ctx, cg, worker)
}

// ensurePVCForVolumeClaimTemplate creates a PVC or expands its storage request.
func (r *CacheGroupReconciler) ensurePVCForVolumeClaimTemplate(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string, vct *corev1.PersistentVolumeClaim) error {
	if vct == nil {
		return fmt.Errorf("volumeClaimTemplate is required for VolumeClaimTemplates type")
	}

	pvcName := common.GenPVCName(vct.Name, workerName)

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: pvcName}, pvc)
	if err == nil {
		if !metav1.IsControlledBy(pvc, cg) {
			return nil
		}
		expectedStorage, ok := vct.Spec.Resources.Requests[corev1.ResourceStorage]
		if !ok {
			return nil
		}
		currentStorage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if expectedStorage.Cmp(currentStorage) <= 0 {
			return nil
		}
		if pvc.Spec.Resources.Requests == nil {
			pvc.Spec.Resources.Requests = corev1.ResourceList{}
		}
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = expectedStorage
		log.FromContext(ctx).Info("expanding PVC for cache group", "pvc", pvc.Name, "storage", expectedStorage.String())
		return r.Update(ctx, pvc)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cg.Namespace,
			Labels: map[string]string{
				common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name),
				common.LabelManagedBy:  common.LabelManagedByValue,
			},
			Annotations: vct.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cg.APIVersion,
					Kind:       cg.Kind,
					Name:       cg.Name,
					UID:        cg.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: vct.Spec,
	}

	return r.Create(ctx, pvc)
}

func (r *CacheGroupReconciler) HandleFinalizer(ctx context.Context, cg *juicefsiov1.CacheGroup) error {
	log := log.FromContext(ctx)

	if !cg.Spec.CleanCache {
		return nil
	}

	expectStates, err := r.parseExpectState(ctx, cg)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: cg.Spec.SecretRef.Name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get secret", "secret", cg.Spec.SecretRef.Name)
			return err
		}
		if cg.Status.FileSystem == "" {
			log.Info("secret not found and file system is unknown, skip cleaning cache", "secret", cg.Spec.SecretRef.Name)
			return nil
		}
		log.Info("secret not found, continue cleaning cache using status", "secret", cg.Spec.SecretRef.Name, "fileSystem", cg.Status.FileSystem)
	}
	for node, expectState := range expectStates {
		podBuilder := builder.NewPodBuilder(cg, secret, node, expectState, false)
		expectWorker := podBuilder.NewCacheGroupWorker(ctx, false)
		if err := r.cleanWorkerCache(ctx, cg, *expectWorker); err != nil {
			log.Error(err, "failed to clean worker cache", "worker", expectWorker.Name)
		}
	}
	return nil
}

// deletePVCForCacheGroup deletes PVCs created by VolumeClaimTemplates.
func (r *CacheGroupReconciler) deletePVCForCacheGroup(ctx context.Context, cg *juicefsiov1.CacheGroup, worker corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, v := range worker.Spec.Volumes {
		if !strings.HasPrefix(v.Name, common.CacheDirVolumeNamePrefix) || v.VolumeSource.PersistentVolumeClaim == nil {
			continue
		}
		pvcName := v.VolumeSource.PersistentVolumeClaim.ClaimName
		pvc := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: pvcName}, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !metav1.IsControlledBy(pvc, cg) {
			continue
		}

		log.Info("deleting PVC for cache group", "pvc", pvc.Name, "cacheGroup", cg.Name)
		if err := r.Delete(ctx, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			log.Error(err, "failed to delete PVC", "pvc", pvc.Name)
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.CacheGroup{}, ctrlbuilder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCG := e.ObjectOld.(*juicefsiov1.CacheGroup).DeepCopy()
				newCG := e.ObjectNew.(*juicefsiov1.CacheGroup).DeepCopy()
				oldCG.Status.Stats, newCG.Status.Stats = nil, nil
				oldCG.Status.CacheSize, newCG.Status.CacheSize = "", ""
				oldCG.Status.CacheCapacity, newCG.Status.CacheCapacity = "", ""
				oldCG.ResourceVersion, newCG.ResourceVersion = "", ""
				oldCG.ManagedFields, newCG.ManagedFields = nil, nil
				return !reflect.DeepEqual(oldCG, newCG)
			},
		})).
		Owns(&corev1.Pod{}, ctrlbuilder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				r.recordWorkerReadiness(e.ObjectOld, e.ObjectNew)
				return true
			},
		})).
		Owns(&corev1.PersistentVolumeClaim{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxCGConcurrentReconciles,
		}).
		Watches(&corev1.Node{}, r.enqueueRequestForNode()).
		Complete(r)
}

func (r *CacheGroupReconciler) eventf(cg *juicefsiov1.CacheGroup, eventType, reason, messageFmt string, args ...any) {
	if r.Recorder != nil {
		r.Recorder.Eventf(cg, eventType, reason, messageFmt, args...)
	}
}

// recordWorkerReadiness records an event on the cache group when its worker becomes ready or not ready.
func (r *CacheGroupReconciler) recordWorkerReadiness(oldObj, newObj client.Object) {
	oldPod, ok1 := oldObj.(*corev1.Pod)
	newPod, ok2 := newObj.(*corev1.Pod)
	if !ok1 || !ok2 || newPod.Labels[common.LabelAppType] != common.LabelWorkerValue || !newPod.DeletionTimestamp.IsZero() {
		return
	}
	owner := metav1.GetControllerOf(newPod)
	if owner == nil || owner.Kind != "CacheGroup" {
		return
	}
	cg := &juicefsiov1.CacheGroup{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: newPod.Namespace, UID: owner.UID}}
	wasReady, ready := utils.IsPodReady(*oldPod), utils.IsPodReady(*newPod)
	switch {
	case !wasReady && ready:
		r.eventf(cg, corev1.EventTypeNormal, "WorkerReady", "Worker %s is ready", newPod.Name)
	case wasReady && !ready:
		r.eventf(cg, corev1.EventTypeWarning, "WorkerNotReady", "Worker %s is not ready: %s",
			newPod.Name, strings.Join(utils.FormatPodNotReadyConditions(*newPod), "; "))
	}
}
