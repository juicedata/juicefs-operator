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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/builder"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
)

// SyncReconciler reconciles a Sync object
type SyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=syncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=syncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=syncs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Sync object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *SyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	sync := &juicefsiov1.Sync{}
	if err := r.Get(ctx, req.NamespacedName, sync); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sync.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	from, err := utils.ParseSyncSink(sync.Spec.From, sync.Name, "FROM")
	if err != nil {
		l.Error(err, "failed to parse from sink")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, sync)
	}
	to, err := utils.ParseSyncSink(sync.Spec.To, sync.Name, "TO")
	if err != nil {
		l.Error(err, "failed to parse to sink")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, sync)
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseWaiting || sync.Status.Phase == "" {
		sync.Status.Phase = juicefsiov1.SyncPhasePreparing
		if err := r.Status().Update(ctx, sync); err != nil {
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhasePreparing {
		// prepare secrets
		if err := r.prepareSyncSecrets(ctx, sync); err != nil {
			l.Error(err, "failed to prepare sync secrets")
			return ctrl.Result{}, err
		}

		builder := builder.NewSyncPodBuilder(sync, from, to)
		if err := r.prepareWorkerPod(ctx, sync, builder); err != nil {
			l.Error(err, "failed to prepare worker pod")
			return ctrl.Result{}, err
		}

		// prepare manager pod
		if err := r.prepareManagerPod(ctx, sync, builder); err != nil {
			l.Error(err, "failed to prepare manager pod")
			return ctrl.Result{}, err
		}

		sync.Status.Phase = juicefsiov1.SyncPhaseProgressing
		if err := r.Status().Update(ctx, sync); err != nil {
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseCompleted {
		// delete worker pod
		labelSelector := client.MatchingLabels{
			common.LabelSync:    sync.Name,
			common.LabelAppType: common.LabelSyncWorkerValue,
		}
		if err := r.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(sync.Namespace), labelSelector); err != nil {
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseProgressing {
		// get manager pod
		managerPod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: common.GenSyncManagerName(sync.Name)}, managerPod); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if managerPod.Status.Phase == corev1.PodSucceeded {
			sync.Status.Phase = juicefsiov1.SyncPhaseCompleted
			if err := r.Status().Update(ctx, sync); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SyncReconciler) prepareSyncSecrets(ctx context.Context, sync *juicefsiov1.Sync) error {
	secretName := common.GenSyncSecretName(sync.Name)
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: secretName}, secret)
	if err == nil || !apierrors.IsNotFound(err) {
		return err
	}
	secret, err = builder.NewSyncSecret(ctx, sync)
	if err != nil {
		return err
	}
	return r.Create(ctx, secret)
}

func (r *SyncReconciler) prepareWorkerPod(ctx context.Context, sync *juicefsiov1.Sync, builder *builder.SyncPodBuilder) error {
	if !utils.IsDistributed(sync) {
		return nil
	}
	log := log.FromContext(ctx)
	labelSelector := client.MatchingLabels{
		common.LabelSync:    sync.Name,
		common.LabelAppType: common.LabelSyncWorkerValue,
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, labelSelector); err != nil {
		return err
	}

	if len(pods.Items) == 0 {
		// create worker pod
		workerPods := builder.NewWorkerPods()
		for _, pod := range workerPods {
			if err := r.Create(ctx, &pod); err != nil {
				return err
			}
		}
	}

	// waiting for worker pod ready
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for worker pod ready")
		default:
			err := r.List(ctx, pods, labelSelector)
			if err != nil {
				log.Error(err, "failed to list worker pods")
				time.Sleep(5 * time.Second)
				continue
			}
			if len(pods.Items) != int(*sync.Spec.Replicas)-1 {
				log.Error(fmt.Errorf("worker pod not ready"), "worker pod not ready")
			}
			ips := make([]string, 0, len(pods.Items))
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodRunning {
					ips = append(ips, pod.Status.PodIP)
				} else {
					log.Info("worker pod not ready", "pod", pod.Name, "status", pod.Status.Phase)
					break
				}
			}
			if len(ips) == int(*sync.Spec.Replicas)-1 {
				log.V(1).Info("sync worker pod ready", "ips", ips)
				builder.UpdateWorkerIPs(ips)
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (r *SyncReconciler) prepareManagerPod(ctx context.Context, sync *juicefsiov1.Sync, builder *builder.SyncPodBuilder) error {
	managerPod := builder.NewManagerPod()
	if err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: managerPod.Name}, &corev1.Pod{}); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, managerPod)
		}
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.Sync{}).
		Owns(&corev1.Pod{}).
		Named("sync").
		Complete(r)
}
