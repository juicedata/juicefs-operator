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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// CacheGroupReconciler reconciles a CacheGroup object
type CacheGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=cachegroups/finalizers,verbs=update
// +kubebuilder:rbac:groups=,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=,resources=pods,verbs=get;list;delete

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
	l.Info("Reconcile CacheGroup")
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

	return ctrl.Result{}, nil
}

// sync synchronizes the cache group workers with the expect states
func (r *CacheGroupReconciler) sync(ctx context.Context, cg *juicefsiov1.CacheGroup, expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate) error {
	log := log.FromContext(ctx)
	// TODO: follow the update strategy
	for node, expectState := range expectStates {
		actualState, err := r.getActualState(ctx, cg, node)
		if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get actual state", "node", node)
			continue
		}
		expectWorker := builder.NewCacheGroupWorker(ctx, cg, node, expectState)
		hash := utils.GenHash(expectWorker)
		expectWorker.Annotations[common.LabelWorkerHash] = hash

		// TODO: if the current worker is not ready, block the next worker update
		if apierrors.IsNotFound(err) || r.compare(expectWorker, actualState) {
			log.Info("cache group worker need to be updated", "worker", expectWorker.Name)
			if err := r.createOrUpdateWorker(ctx, actualState, expectWorker); err != nil {
				log.Error(err, "failed to create or update cache group worker", "worker", expectWorker.Name)
				return err
			}
			// Wait for the worker to be ready
			if err := r.waitForWorkerReady(ctx, cg, expectWorker.Name); err != nil {
				log.Error(err, "failed to wait for worker to be ready", "worker", expectWorker.Name)
			}
			break
		}
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

func (r *CacheGroupReconciler) compare(expect, actual *corev1.Pod) bool {
	if actual == nil {
		return true
	}
	if expect.Annotations[common.LabelWorkerHash] != actual.Annotations[common.LabelWorkerHash] {
		return true
	}
	if !actual.DeletionTimestamp.IsZero() {
		return true
	}
	// TODO: check pod status
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
		expectStates[node.Name] = cg.Spec.Worker.Template
		for _, overwrite := range cg.Spec.Worker.Overwrite {
			if utils.SliceContains(overwrite.Nodes, node.Name) {
				// TODO: merge and overwrite spec
				break
			}
		}
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
	if actual == nil {
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
	log := log.FromContext(ctx)
	worker := corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: oldWorker.Namespace, Name: oldWorker.Name}, &worker); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	err := r.Delete(ctx, &worker)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete old cache group worker", "worker", worker.Name)
			return err
		}
	}
	// wait for the old worker to be deleted
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		err := r.Get(ctx, client.ObjectKey{Namespace: oldWorker.Namespace, Name: oldWorker.Name}, &worker)
		if apierrors.IsNotFound(err) {
			break
		}
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout waiting for old cache group worker to be deleted")
		}
		time.Sleep(time.Second)
	}
	log.Info("old cache group worker deleted, created new one", "worker", expectWorker.Name)
	return r.Create(ctx, expectWorker)
}

func (r *CacheGroupReconciler) deleteCacheGroupWorker(ctx context.Context, worker *corev1.Pod) error {
	return client.IgnoreNotFound(r.Delete(ctx, worker))
}

func (r *CacheGroupReconciler) listActualWorkers(ctx context.Context, cg *juicefsiov1.CacheGroup) ([]corev1.Pod, error) {
	workers := corev1.PodList{}
	if err := r.List(ctx, &workers, client.MatchingLabels(map[string]string{common.LabelCacheGroup: cg.Name})); err != nil {
		return nil, err
	}
	return workers.Items, nil
}

func (r *CacheGroupReconciler) removeRedundantWorkers(
	ctx context.Context,
	expectStates map[string]juicefsiov1.CacheGroupWorkerTemplate,
	actualWorks []corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, worker := range actualWorks {
		if _, ok := expectStates[worker.Spec.NodeName]; !ok {
			log.Info("found redundant cache group worker, delete it", "worker", worker.Name)
			if err := r.deleteCacheGroupWorker(ctx, &worker); err != nil {
				log.Error(err, "failed to delete cache group worker", "worker", worker.Name)
				return err
			}
		}
	}
	return nil
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
	newStatus.ReadyWorker = int32(len(actualWorks))
	newStatus.ExpectWorker = int32(len(expectStates))
	newStatus.ReadyStr = fmt.Sprintf("%d/%d", newStatus.ReadyWorker, newStatus.ExpectWorker)
	if newStatus.ExpectWorker != int32(len(expectStates)) {
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
			if worker.Status.Phase == corev1.PodRunning {
				log.Info("worker is ready", "worker", workerName)
				return nil
			}
			time.Sleep(time.Second)
		}
	}
}

func (r *CacheGroupReconciler) HandleFinalizer(ctx context.Context, cg *juicefsiov1.CacheGroup) error {
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
