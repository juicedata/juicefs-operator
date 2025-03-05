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
	"strings"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SyncReconciler reconciles a Sync object
type SyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=syncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=syncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=syncs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete;create;watch;deletecollection
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;delete;create;watch;update
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create

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
	if from.FilesFrom != nil && utils.CompareEEImageVersion(sync.Spec.Image, "5.1.10") < 0 {
		err := fmt.Errorf("filesFrom is only supported in JuiceFS EE 5.1.10 or later")
		l.Error(err, "")
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

	if sync.Status.Phase == juicefsiov1.SyncPhasePending || sync.Status.Phase == "" {
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

		sync.Status.StartAt = &metav1.Time{Time: time.Now()}
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
		if sync.Spec.TTLSecondsAfterFinished != nil {
			completedAt := sync.Status.CompletedAt
			if completedAt == nil {
				return ctrl.Result{}, nil
			}
			since := float64(*sync.Spec.TTLSecondsAfterFinished) - time.Since(completedAt.Time).Seconds()
			if since <= 0 {
				l.Info("sync ttl is expired, deleted")
				if err := r.Delete(ctx, sync); err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
			}
			return ctrl.Result{RequeueAfter: time.Second*time.Duration(since) + 1}, nil
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseProgressing {
		// get manager pod
		managerPod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: common.GenSyncManagerName(sync.Name)}, managerPod); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status, err := r.calculateSyncStats(ctx, sync, managerPod)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !reflect.DeepEqual(sync.Status, status) {
			sync.Status = status
			return ctrl.Result{RequeueAfter: 3 * time.Second}, utils.IgnoreConflict(r.Status().Update(ctx, sync))
		}
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SyncReconciler) calculateSyncStats(ctx context.Context, sync *juicefsiov1.Sync, managerPod *corev1.Pod) (juicefsiov1.SyncStatus, error) {
	l := log.FromContext(ctx)
	status := sync.Status
	if managerPod.Status.Phase == corev1.PodSucceeded {
		finishLog, err := utils.LogPod(ctx, sync.Namespace, common.GenSyncManagerName(sync.Name), common.SyncNamePrefix, 2)
		if err != nil {
			l.Error(err, "failed to get manager pod last logs")
			return status, err
		}
		if len(finishLog) > 0 {
			status.FinishLog = strings.Split(finishLog, "\n")[0]
		}
		status.Phase = juicefsiov1.SyncPhaseCompleted
		status.CompletedAt = &metav1.Time{Time: time.Now()}
		statsMap, err := utils.ParseLog(status.FinishLog)
		if err != nil {
			l.Error(err, "failed to parse log")
		} else {
			stats := juicefsiov1.SyncStats{}
			if handled, ok := statsMap["found"]; ok {
				stats.Handled = handled
			}
			if copied, ok := statsMap["copied"]; ok {
				stats.Copied = copied
			}
			if failed, ok := statsMap["failed"]; ok {
				stats.Failed = failed
			}
			if skipped, ok := statsMap["skipped"]; ok {
				stats.Skipped = skipped
			}
			if copiedBytes, ok := statsMap["copied_bytes"]; ok {
				stats.CopiedBytes = copiedBytes
			}
			if checked, ok := statsMap["checked"]; ok {
				stats.Checked = checked
			}
			if lost, ok := statsMap["lost"]; ok {
				stats.Lost = lost
			}
			if stats.Lost > 0 || stats.Failed > 0 {
				status.Phase = juicefsiov1.SyncPhaseFailed
			}
			if stats.Handled > 0 {
				status.Progress = fmt.Sprintf("%.2f%%", float64(stats.Handled-stats.Failed-stats.Lost)/float64(stats.Handled)*100)
			}
			status.Stats = stats
		}
		return status, nil
	}
	if status.Stats.LastUpdated != nil && time.Since(status.Stats.LastUpdated.Time) < 3*time.Second {
		return status, nil
	}
	if utils.IsPodReady(*managerPod) {
		metrics, err := utils.FetchMetrics(ctx, sync)
		if err != nil {
			return status, nil
		}
		stats := juicefsiov1.SyncStats{
			Handled:     int64(metrics["juicefs_sync_handled"]),
			Copied:      int64(metrics["juicefs_sync_copied"]),
			Failed:      int64(metrics["juicefs_sync_failed"]),
			Skipped:     int64(metrics["juicefs_sync_skipped"]),
			Checked:     int64(metrics["juicefs_sync_checked"]),
			CopiedBytes: int64(metrics["juicefs_sync_copied_bytes"]),
			Scanned:     int64(metrics["juicefs_sync_scanned"]),
			LastUpdated: &metav1.Time{Time: time.Now()},
		}
		if stats.Scanned > 0 {
			status.Progress = fmt.Sprintf("%.2f%%", float64(stats.Handled)/float64(stats.Scanned)*100)
		}
		status.Stats = stats
		return status, nil
	}
	return status, nil
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
				log.Info("worker pods not ready", "expect", *sync.Spec.Replicas-1, "actual", len(pods.Items))
				time.Sleep(5 * time.Second)
				continue
			}
			ips := make([]string, 0, len(pods.Items))
			for _, pod := range pods.Items {
				if utils.IsPodReady(pod) {
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
