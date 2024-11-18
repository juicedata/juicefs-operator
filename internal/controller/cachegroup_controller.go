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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/builder"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	// TODO(user): your logic here
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
		backUpWorker := r.asBackupWorkerOrNot(cg, actualState)
		podBuilder := builder.NewPodBuilder(cg, secret, node, expectState, backUpWorker)
		expectWorker := podBuilder.NewCacheGroupWorker(ctx)

		if r.actualShouldbeUpdate(updateStrategyType, expectWorker, actualState) {
			if numUnavailable >= maxUnavailable {
				log.V(1).Info("maxUnavailable reached, skip updating worker, waiting for next reconciler", "worker", expectWorker.Name)
				break
			}
			wg.Add(1)
			numUnavailable++
			go func() {
				defer wg.Done()
				if err := r.createOrUpdateWorker(ctx, actualState, expectWorker); err != nil {
					log.Error(err, "failed to create or update worker", "worker", expectWorker.Name)
					errCh <- err
					return
				}
				// Wait for the worker to be ready
				if err := r.waitForWorkerReady(ctx, cg, expectWorker.Name); err != nil {
					log.Error(err, "failed to wait for worker to be ready", "worker", expectWorker.Name)
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
	if err := r.removeRedundantWorkers(ctx, expectStates, actualWorks); err != nil {
		log.Error(err, "failed to remove redundant")
		return err
	}

	// calculate status
	newStatus := r.calculateStatus(cg, expectStates, actualWorks)
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
	if expect.Annotations[common.LabelWorkerHash] != actual.Annotations[common.LabelWorkerHash] {
		return true
	}
	if !actual.DeletionTimestamp.IsZero() {
		return true
	}
	return false
}

func (r *CacheGroupReconciler) parseExpectState(ctx context.Context, cg *juicefsiov1.CacheGroup) (map[string]juicefsiov1.CacheGroupWorkerTemplate, error) {
	expectAppliedNodes := corev1.NodeList{}
	err := r.List(ctx, &expectAppliedNodes, client.MatchingLabels(cg.Spec.Worker.Template.NodeSelector))
	if err != nil {
		return nil, err
	}
	expectStates := map[string]juicefsiov1.CacheGroupWorkerTemplate{}
	for _, node := range expectAppliedNodes.Items {
		expectState := cg.Spec.Worker.Template.DeepCopy()
		for _, overwrite := range cg.Spec.Worker.Overwrite {
			if utils.SliceContains(overwrite.Nodes, node.Name) ||
				(overwrite.NodeSelector != nil && utils.NodeSelectorContains(overwrite.NodeSelector, node.Labels)) {
				builder.MergeCacheGrouopWorkerTemplate(expectState, overwrite)
			}
		}
		expectStates[node.Name] = *expectState
	}
	return expectStates, nil
}

func (r *CacheGroupReconciler) getActualState(ctx context.Context, cg *juicefsiov1.CacheGroup, nodename string) (*corev1.Pod, error) {
	worker := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: common.GenWorkerName(cg.Name, nodename)}, worker); err != nil {
		return nil, err
	}
	return worker, nil
}

func (r *CacheGroupReconciler) createOrUpdateWorker(ctx context.Context, actual, expect *corev1.Pod) error {
	log := log.FromContext(ctx).WithValues("worker", expect.Name)
	if actual == nil {
		log.Info("create worker")
		return r.createCacheGroupWorker(ctx, expect)
	}
	return r.updateCacheGroupWorker(ctx, actual, expect)
}

func (r *CacheGroupReconciler) createCacheGroupWorker(ctx context.Context, expectWorker *corev1.Pod) error {
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
	if err := r.List(ctx, &workers, client.MatchingLabels(map[string]string{common.LabelCacheGroup: cg.Name})); err != nil {
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
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, worker := range actualWorks {
		if worker.DeletionTimestamp != nil {
			continue
		}
		if _, ok := expectStates[worker.Spec.NodeName]; !ok {
			// if the worker in delete state,
			cacheBytes, err := utils.GetWorkerCacheBlocksBytes(ctx, worker, common.MountPoint)
			if err != nil {
				log.Error(err, "failed to get worker cache blocks bytes", "worker", worker.Name)
				continue
			}
			if cacheBytes > 0 {
				log.Info("found redundant worker, but it still has cache blocks, tweak the group weight to zero", "worker", worker.Name)
				if err := r.gracefulShutdownWorker(ctx, &worker); err != nil {
					log.Error(err, "failed to graceful shutdown worker", "worker", worker)
				}
			} else {
				log.Info("found redundant worker, delete it", "worker", worker.Name)
				if err := r.deleteCacheGroupWorker(ctx, &worker, false); err != nil {
					log.Error(err, "failed to delete worker", "worker", worker.Name)
					return err
				}
			}
		}
	}
	return nil
}

// change pod options `group-weight` to zero, delete and recreate the worker pod
func (r *CacheGroupReconciler) gracefulShutdownWorker(ctx context.Context, worker *corev1.Pod) error {
	if _, ok := worker.Annotations[common.AnnoWaitingDeleteWorker]; ok {
		return nil
	}
	if err := r.deleteCacheGroupWorker(ctx, worker, true); err != nil {
		return err
	}
	builder.UpdateWorkerGroupWeight(worker, 0)
	worker.ResourceVersion = ""
	worker.Annotations[common.AnnoWaitingDeleteWorker] = time.Now().Format(time.RFC3339)
	if err := r.Create(ctx, worker); err != nil {
		return err
	}
	return nil
}

func (r *CacheGroupReconciler) asBackupWorkerOrNot(cg *juicefsiov1.CacheGroup, actual *corev1.Pod) bool {
	// If it is a new node and there are already 2 or more worker nodes
	// then this node is a backup worker.
	if actual == nil {
		return cg.Status.ReadyWorker >= 2
	}
	// If this node has been acting as a backup node for 10 minutes
	// then this node is a normal worker.
	if v, ok := actual.Annotations[common.AnnoBackupWorker]; ok {
		backupAt := utils.MustParseTime(v)
		// TODO: 10 minutes should be configurable
		return time.Since(backupAt) < 10*time.Minute
	}
	return false
}

func (r *CacheGroupReconciler) calculateStatus(
	cg *juicefsiov1.CacheGroup,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod) juicefsiov1.CacheGroupStatus {
	newStatus := cg.Status
	if len(expectStates) == 0 {
		newStatus.ReadyStr = "-"
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

func (r *CacheGroupReconciler) HandleFinalizer(ctx context.Context, cg *juicefsiov1.CacheGroup) error {
	// TODO: clean cache
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.CacheGroup{}).
		Owns(&corev1.Pod{}).
		Watches(&corev1.Node{}, r.enqueueRequestForNode()).
		Complete(r)
}
