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
// +kubebuilder:rbac:groups=,resources=pods,verbs=get;list

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

		if apierrors.IsNotFound(err) {
			log.Info("cache group worker not found, create it", "node", node)
			if err := r.createCacheGroupWorker(ctx, cg, node, expectState); err != nil {
				log.Error(err, "failed to create cache group worker", "node", node)
			}
			// Wait for the worker to be ready
			if err := r.waitForWorkerReady(ctx, cg, node); err != nil {
				log.Error(err, "failed to wait for worker to be ready", "node", node)
			}
			continue
		}
		if r.compare(expectState, actualState) {
			log.Info("cache group worker need to be updated", "node", node)
			if err := r.updateCacheGroupWorker(ctx, cg, node, expectState); err != nil {
				log.Error(err, "failed to update cache group worker", "node", node)
			}
			// Wait for the worker to be ready
			if err := r.waitForWorkerReady(ctx, cg, node); err != nil {
				log.Error(err, "failed to wait for worker to be ready", "node", node)
			}
			continue
		}
	}

	// list all cache group workers and delete the redundant ones
	actualWorks, err := r.listActualWorkerNodes(ctx, cg)
	if err != nil {
		log.Error(err, "failed to list actual worker nodes")
		return err
	}
	for _, node := range actualWorks {
		if _, ok := expectStates[node]; !ok {
			log.Info("found redundant cache group worker, delete it", "worker", common.GenWorkerName(cg.Name, node))
			if err := r.deleteCacheGroupWorker(ctx, cg, node); err != nil {
				if !apierrors.IsNotFound(err) {
					log.Error(err, "failed to delete cache group worker", "worker", common.GenWorkerName(cg.Name, node))
					return err
				}
			}
		}
	}

	// update status
	if len(expectStates) == 0 {
		cg.Status.ReadyStr = "-"
		cg.Status.Phase = juicefsiov1.CacheGroupPhaseWaiting
		log.Info("no worker expected, waiting for spec update")
		return utils.IgnoreConflict(r.Status().Update(ctx, cg))
	}
	cg.Status.ReadyWorker = int32(len(actualWorks))
	cg.Status.ExpectWorker = int32(len(expectStates))
	cg.Status.ReadyStr = fmt.Sprintf("%d/%d", cg.Status.ReadyWorker, cg.Status.ExpectWorker)
	if cg.Status.ExpectWorker != int32(len(expectStates)) {
		cg.Status.Phase = juicefsiov1.CacheGroupPhaseProgressing
	} else {
		cg.Status.Phase = juicefsiov1.CacheGroupPhaseReady
	}
	cg.Status.CacheGroup = builder.GenCacheGroupName(cg)
	return utils.IgnoreConflict(r.Status().Update(ctx, cg))
}

func (r *CacheGroupReconciler) compare(expect juicefsiov1.CacheGroupWorkerTemplate, actual corev1.Pod) bool {
	if utils.GenHash(expect) != actual.Annotations[common.LabelWorkerHash] {
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

func (r *CacheGroupReconciler) getActualState(ctx context.Context, cg *juicefsiov1.CacheGroup, nodename string) (corev1.Pod, error) {
	worker := corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: common.GenWorkerName(cg.Name, nodename)}, &worker); err != nil {
		return worker, err
	}
	return worker, nil
}

func (r *CacheGroupReconciler) createCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, nodename string, spec juicefsiov1.CacheGroupWorkerTemplate) error {
	worker := builder.NewCacheGroupWorker(cg, nodename, spec)
	err := r.Create(ctx, worker)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *CacheGroupReconciler) updateCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, nodename string, _ juicefsiov1.CacheGroupWorkerTemplate) error {
	log := log.FromContext(ctx)
	worker := corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: common.GenWorkerName(cg.Name, nodename)}, &worker); err != nil && !apierrors.IsNotFound(err) {
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
		if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: common.GenWorkerName(cg.Name, nodename)}, &worker); err != nil {
			if apierrors.IsNotFound(err) {
				break
			}
		}
		time.Sleep(time.Second)
	}
	log.Info("old cache group worker deleted, created new one", "worker", worker.Name)
	return r.Create(ctx, builder.NewCacheGroupWorker(cg, nodename, cg.Spec.Worker.Template))
}

func (r *CacheGroupReconciler) deleteCacheGroupWorker(ctx context.Context, cg *juicefsiov1.CacheGroup, nodename string) error {
	worker := corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cg.Namespace, Name: common.GenWorkerName(cg.Name, nodename)}, &worker); err != nil {
		return client.IgnoreNotFound(err)
	}
	return r.Delete(ctx, &worker)
}

func (r *CacheGroupReconciler) listActualWorkerNodes(ctx context.Context, cg *juicefsiov1.CacheGroup) ([]string, error) {
	workers := corev1.PodList{}
	if err := r.List(ctx, &workers, client.MatchingLabels(map[string]string{common.LabelCacheGroup: cg.Name})); err != nil {
		return nil, err
	}
	nodes := []string{}
	for _, worker := range workers.Items {
		nodes = append(nodes, worker.Spec.NodeName)
	}
	return nodes, nil
}

func (r *CacheGroupReconciler) waitForWorkerReady(ctx context.Context, cg *juicefsiov1.CacheGroup, node string) error {
	log := log.FromContext(ctx)
	workerName := common.GenWorkerName(cg.Name, node)
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
