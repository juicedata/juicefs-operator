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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// CacheGroupReconciler reconciles a CacheGroup object
type CacheGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;create;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create;delete;get;list;update;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

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
				return ctrl.Result{}, err
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
	if cg.Status.BackUpWorker > 0 || cg.Status.WaitingDeletedWorker > 0 {
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
	updateStrategyType, maxUnavailable := utils.ParseUpdateStrategy(cg.Spec.UpdateStrategy, len(expectStates))
	wg := sync.WaitGroup{}
	errCh := make(chan error, 2*maxUnavailable)
	numUnavailable := 0
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
	log.V(1).Info("sync worker to expect states", "expectStates", len(expectStates))
	for node, expectState := range expectStates {
		actualState, err := r.getActualState(ctx, cg, node)
		if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get actual state", "node", node)
			continue
		}
		groupBackUp := r.shouldAddGroupBackupOrNot(cg, actualState, expectState.Image)
		podBuilder := builder.NewPodBuilder(cg, secret, node, expectState, groupBackUp)
		expectWorker := podBuilder.NewCacheGroupWorker(ctx)

		if r.actualShouldbeUpdate(updateStrategyType, expectWorker, actualState) {
			if numUnavailable >= maxUnavailable {
				log.V(1).Info("maxUnavailable reached, skip updating worker, waiting for next reconciler", "worker", expectWorker.Name)
				break
			}
			wg.Add(1)
			if actualState != nil {
				numUnavailable++
			}
			go func() {
				defer wg.Done()
				if groupBackUp {
					log.V(1).Info("new worker added, add group-backup option", "worker", expectWorker.Name)
				}
				if err := r.createOrUpdateWorker(ctx, cg, expectState, actualState, expectWorker); err != nil {
					log.Error(err, "failed to create or update worker", "worker", expectWorker.Name)
					errCh <- err
					return
				}
				// Wait for the worker to be ready
				if err := r.waitForWorkerReady(ctx, cg, expectWorker.Name); err != nil {
					log.Info("worker is not ready, waiting for next reconciler", "worker", expectWorker.Name, "err", err)
					errCh <- err
					return
				}
			}()
		} else {
			log.V(1).Info("worker is up to date", "worker", expectWorker.Name)
		}
	}
	wg.Wait()

	close(errCh)
	// collect errors if any for proper reporting/retry logic in the controller
	errors := []error{}
	for err := range errCh {
		errors = append(errors, err)
	}
	if len(errors) > 0 {
		return utilerrors.NewAggregate(errors)
	}

	// list all cache group workers and delete the redundant ones
	actualWorks, err := r.listActualWorkers(ctx, cg)
	if err != nil {
		log.Error(err, "failed to list actual worker nodes")
		return err
	}
	if err := r.removeRedundantWorkers(ctx, cg, expectStates, actualWorks); err != nil {
		log.Error(err, "failed to remove redundant")
		return err
	}

	// calculate status
	newStatus := r.calculateStatus(cg, string(secret.Data["name"]), expectStates, actualWorks)
	if !reflect.DeepEqual(cg.Status, newStatus) {
		cg.Status = newStatus
		return utils.IgnoreConflict(r.Status().Update(ctx, cg))
	}
	return nil
}

func (r *CacheGroupReconciler) actualShouldbeUpdate(updateStrategyType appsv1.DaemonSetUpdateStrategyType, expect, actual *corev1.Pod) bool {
	if actual == nil {
		return true
	}
	if updateStrategyType == appsv1.OnDeleteDaemonSetStrategyType {
		return false
	}
	// pod may be evicted or some exit without restarting., so we need to recreate it
	if actual.Status.Phase == corev1.PodFailed || actual.Status.Phase == corev1.PodUnknown || actual.Status.Phase == corev1.PodSucceeded {
		return true
	}
	if expect.Annotations[common.LabelWorkerHash] != actual.Annotations[common.LabelWorkerHash] {
		return true
	}
	if !actual.DeletionTimestamp.IsZero() {
		return true
	}
	return false
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
	log := log.FromContext(ctx)
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
	expectStates := r.parseExpectStateWithNode(cg, checkNodes.Items)
	for _, node := range checkNodes.Items {
		state := expectStates[node.Name]
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				NodeSelector: state.NodeSelector,
				Affinity:     state.Affinity,
				Tolerations:  state.Tolerations,
			},
		}
		fitsNodeAffinity, fitsTaints := utils.PodShouldBeOnNode(pod, &node, node.Spec.Taints)
		if !fitsNodeAffinity || !fitsTaints {
			log.V(1).Info("node does not fit the predicates, ignore", "node", node.Name, "fitsNodeAffinity", fitsNodeAffinity, "fitsTaints", fitsTaints)
			delete(expectStates, node.Name)
		}
	}
	return expectStates, nil
}

func (r *CacheGroupReconciler) getActualState(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string) (*corev1.Pod, error) {
	worker := &corev1.Pod{}
	var podName string
	if cg.Spec.Replicas != nil {
		podName = workerName
	} else {
		podName = common.GenWorkerName(cg.Name, workerName)
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: podName}, worker); err != nil {
		return nil, err
	}
	return worker, nil
}

func (r *CacheGroupReconciler) createOrUpdateWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, spec juicefsiov1.CacheGroupWorkerTemplate, actual, expect *corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", expect.Name)
	if actual == nil {
		log.Info("create worker")
		return r.createCacheGroupWorker(ctx, cg, spec, expect)
	}
	return r.updateCacheGroupWorker(ctx, actual, expect)
}

// ensurePVCsForWorker ensures all PVCs for a worker are created
func (r *CacheGroupReconciler) ensurePVCsForWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string, spec juicefsiov1.CacheGroupWorkerTemplate) error {
	for i, cacheDir := range spec.CacheDirs {
		if cacheDir.Type == juicefsiov1.CacheDirTypeVolumeClaimTemplates {
			if err := r.createPVCForVolumeClaimTemplate(ctx, cg, workerName, cacheDir.VolumeClaimTemplate); err != nil {
				return fmt.Errorf("failed to create PVC for cache dir %d: %w", i, err)
			}
		}
	}
	return nil
}

func (r *CacheGroupReconciler) createCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, spec juicefsiov1.CacheGroupWorkerTemplate, expectWorker *corev1.Pod) error {
	if err := r.ensurePVCsForWorker(ctx, cg, expectWorker.Name, spec); err != nil {
		return fmt.Errorf("failed to ensure PVCs for worker %s: %w", expectWorker.Name, err)
	}

	err := r.Create(ctx, expectWorker)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *CacheGroupReconciler) updateCacheGroupWorker(ctx context.Context, oldWorker, expectWorker *corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", expectWorker.Name)
	log.Info("worker spec changed, delete old and create new one")
	if err := r.deleteCacheGroupWorker(ctx, oldWorker, true); err != nil {
		log.Error(err, "failed to delete old worker")
		return err
	}
	log.Info("old worker deleted, created new one")
	return r.Create(ctx, expectWorker)
}

// deleteCacheGroupWorker deletes a cache group worker pod. If the `waiting` is true,
// it waits until the pod is confirmed to be deleted or a timeout occurs.
func (r *CacheGroupReconciler) deleteCacheGroupWorker(ctx context.Context, worker *corev1.Pod, waiting bool) error {
	err := r.Delete(ctx, worker)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
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
	if err := r.List(ctx, &workers, client.MatchingLabels(map[string]string{common.LabelCacheGroup: utils.TruncateLabelValue(cg.Name)})); err != nil {
		return nil, err
	}
	return workers.Items, nil
}

// removeRedundantWorkers deletes the redundant workers
// if the worker still has cache blocks, tweak the group weight to zero, waiting for data redistribution, then delete it
// if the worker has no cache blocks, delete it directly
// if the worker not ready delete it directly
func (r *CacheGroupReconciler) removeRedundantWorkers(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, worker := range actualWorks {
		var workerKey string
		if cg.Spec.Replicas != nil {
			workerKey = worker.Name
		} else {
			workerKey = worker.Spec.NodeName
		}
		if _, ok := expectStates[workerKey]; ok {
			continue
		}
		if worker.DeletionTimestamp != nil {
			continue
		}

		if cg.Status.ReadyWorker > 1 {
			delete, err := r.gracefulShutdownWorker(ctx, cg, &worker)
			if err != nil {
				log.Error(err, "failed to graceful shutdown worker", "worker", worker)
				return err
			}
			if !delete {
				continue
			}
		}

		log.Info("found redundant worker, delete it", "worker", worker.Name)
		if err := r.deleteCacheGroupWorker(ctx, &worker, false); err != nil {
			log.Error(err, "failed to delete worker", "worker", worker.Name)
			return err
		}
		if err := r.cleanWorkerCache(ctx, cg, worker); err != nil {
			log.Error(err, "failed to clean worker cache", "worker", worker.Name)
			return err
		}
	}
	return nil
}

// change pod options `group-weight` to zero, delete and recreate the worker pod
func (r *CacheGroupReconciler) gracefulShutdownWorker(
	ctx context.Context,
	cg *juicefsiov1.CacheGroup,
	worker *corev1.Pod) (delete bool, err error) {
	log := log.FromContext(ctx)
	isReady := utils.IsPodReady(*worker) && utils.IsMountPointReady(ctx, *worker, common.MountPoint)
	if !isReady {
		if _, ok := worker.Annotations[common.AnnoWaitingDeleteWorker]; ok {
			return false, nil
		}
		return true, nil
	}

	cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, *worker, common.MountPoint)
	if err != nil {
		log.Error(err, "failed to get worker cache blocks bytes", "worker", worker.Name)
		return false, err
	}

	if cacheBytes <= 0 {
		log.V(1).Info("redundant worker has no cache blocks, delete it", "worker", worker.Name)
		return true, nil
	}

	if v, ok := worker.Annotations[common.AnnoWaitingDeleteWorker]; ok {
		waitingAt := utils.MustParseTime(v)
		if time.Since(waitingAt) > utils.GetWaitingDeletedMaxDuration(cg.Spec.WaitingDeletedMaxDuration) {
			log.Info("redundant worker still has cache blocks, waiting for data migration timeout, delete it", "worker", worker.Name)
			return true, nil
		}
		// already set group-weight to 0
		return false, nil
	}
	log.V(1).Info("redundant worker has cache blocks, recreate to set group-weight to 0", "worker", worker.Name, "cacheBytes", cacheBytes)
	if err := r.deleteCacheGroupWorker(ctx, worker, true); err != nil {
		return false, err
	}
	builder.UpdateWorkerGroupWeight(worker, 0)
	worker.ResourceVersion = ""
	worker.Annotations[common.AnnoWaitingDeleteWorker] = time.Now().Format(time.RFC3339)
	if err := r.Create(ctx, worker); err != nil {
		return false, err
	}
	return false, err
}

const minBackupWorkerDuration = time.Second

func (r *CacheGroupReconciler) shouldAddGroupBackupOrNot(cg *juicefsiov1.CacheGroup, actual *corev1.Pod, newImage string) bool {
	if utils.CompareEEImageVersion(newImage, "5.1.0") < 0 {
		return false
	}
	duration := utils.GetBackupWorkerDuration(cg.Spec.BackupDuration)
	if duration <= minBackupWorkerDuration {
		return false
	}

	// If it is a new node and there are already 1 or more worker nodes
	// then this node should add group-backup.
	if actual == nil {
		return cg.Status.ReadyWorker >= 1
	}
	// If this node has been added group-backup for x(default 10m) minutes
	// then this node is a normal worker.
	if v, ok := actual.Annotations[common.AnnoBackupWorker]; ok {
		backupAt := utils.MustParseTime(v)
		return time.Since(backupAt) < duration
	}
	return false
}

func (r *CacheGroupReconciler) calculateStatus(
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
		newStatus.Phase = juicefsiov1.CacheGroupPhaseWaiting
		return newStatus
	}
	backupWorker := 0
	waitingDeletedWorker := 0
	for _, worker := range actualWorks {
		if _, ok := worker.Annotations[common.AnnoBackupWorker]; ok {
			backupWorker++
		}
		if _, ok := worker.Annotations[common.AnnoWaitingDeleteWorker]; ok {
			waitingDeletedWorker++
		}
	}
	newStatus.ReadyWorker = int32(len(actualWorks))
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

func (r *CacheGroupReconciler) waitForWorkerReady(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string) error {
	log := log.FromContext(ctx)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for worker to be ready")
		default:
			worker := corev1.Pod{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: workerName}, &worker); err != nil {
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}
			if utils.IsPodReady(worker) && utils.IsMountPointReady(ctx, worker, common.MountPoint) {
				log.Info("worker is ready", "worker", worker.Name)
				return nil
			}
			time.Sleep(time.Second)
		}
	}
}

func (r *CacheGroupReconciler) cleanWorkerCache(ctx context.Context, cg *juicefsiov1.CacheGroup, worker corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", worker.Name)
	if !cg.Spec.CleanCache {
		return nil
	}

	if cg.Spec.Replicas != nil {
		return r.deletePVCForCacheGroup(ctx, cg, worker)
	}

	job := builder.NewCleanCacheJob(*cg, worker)
	if job == nil {
		return nil
	}
	log.Info("worker is to be deleted, create job to clean cache", "job", job.Name)
	err := r.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name}, &batchv1.Job{})
	if err == nil {
		log.Info("clean cache job already exists", "job", job.Name)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		log.Error(err, "failed to get clean cache job", "job", job.Name)
		return err
	}
	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "failed to create clean cache job", "job", job.Name)
		return err
	}
	return nil
}

// createPVCForVolumeClaimTemplate creates a PVC for a VolumeClaimTemplate
func (r *CacheGroupReconciler) createPVCForVolumeClaimTemplate(ctx context.Context, cg *juicefsiov1.CacheGroup, workerName string, vct *corev1.PersistentVolumeClaim) error {
	if vct == nil {
		return fmt.Errorf("volumeClaimTemplate is required for VolumeClaimTemplates type")
	}

	pvcName := common.GenPVCName(vct.Name, workerName)

	// Check if PVC already exists
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: pvcName}, pvc)
	if err == nil {
		// PVC already exists, no need to create
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Create new PVC based on the template
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
		log.Error(err, "failed to get secret", "secret", cg.Spec.SecretRef.Name)
		return err
	}
	for node, expectState := range expectStates {
		podBuilder := builder.NewPodBuilder(cg, secret, node, expectState, false)
		expectWorker := podBuilder.NewCacheGroupWorker(ctx)
		if err := r.cleanWorkerCache(ctx, cg, *expectWorker); err != nil {
			log.Error(err, "failed to clean worker cache", "worker", expectWorker.Name)
		}
	}
	return nil
}

// deletePVCForCacheGroup deletes a PVC created by VolumeClaimTemplates for a cache group
func (r *CacheGroupReconciler) deletePVCForCacheGroup(ctx context.Context, cg *juicefsiov1.CacheGroup, worker corev1.Pod) error {
	pvcName := ""
	for _, v := range worker.Spec.Volumes {
		if v.VolumeSource.PersistentVolumeClaim != nil {
			pvcName = v.VolumeSource.PersistentVolumeClaim.ClaimName
			break
		}
	}
	if pvcName == "" {
		return nil
	}
	log := log.FromContext(ctx)
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: pvcName}, pvc); err != nil {
		return client.IgnoreNotFound(err)
	}

	log.Info("deleting PVC for cache group", "pvc", pvc.Name, "cacheGroup", cg.Name)
	if err := r.Delete(ctx, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete PVC", "pvc", pvc.Name)
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.CacheGroup{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxCGConcurrentReconciles,
		}).
		Watches(&corev1.Node{}, r.enqueueRequestForNode()).
		Complete(r)
}
