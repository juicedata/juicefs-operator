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

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
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

	l.V(1).Info("sync reconcile")
	sync := &juicefsiov1.Sync{}
	if err := r.Get(ctx, req.NamespacedName, sync); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get sync")
		return ctrl.Result{}, err
	}

	if !sync.GetDeletionTimestamp().IsZero() {
		l.V(1).Info("sync is being deleted")
		return ctrl.Result{}, nil
	}

	if (sync.Spec.From.JuiceFS != nil && sync.Spec.To.JuiceFSCE != nil) ||
		(sync.Spec.To.JuiceFS != nil && sync.Spec.From.JuiceFSCE != nil) {
		err := fmt.Errorf("%s", "Sync between the ce version and the ee version sync is not supported yet")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}

	from, err := utils.ParseSyncSink(sync.Spec.From, sync.Name, "FROM")
	if err != nil {
		l.Error(err, "failed to parse from sink")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}
	if sync.Spec.From.JuiceFS != nil && from.FilesFrom != nil && utils.CompareEEImageVersion(sync.Spec.Image, "5.1.10") < 0 {
		err := fmt.Errorf("filesFrom is only supported in JuiceFS EE 5.1.10 or later")
		l.Error(err, "")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}

	if sync.Spec.From.JuiceFSCE != nil && from.FilesFrom != nil && utils.CompareImageVersion(sync.Spec.Image, "1.3.0") < 0 {
		err := fmt.Errorf("filesFrom is only supported in JuiceFS CE 1.3.0 or later")
		l.Error(err, "")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}
	to, err := utils.ParseSyncSink(sync.Spec.To, sync.Name, "TO")
	if err != nil {
		l.Error(err, "failed to parse to sink")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}

	// Reject sync when both from and to are HDFS
	if strings.HasPrefix(from.Uri, "hdfs://") && strings.HasPrefix(to.Uri, "hdfs://") {
		err := fmt.Errorf("HDFS-to-HDFS sync is not supported")
		l.Error(err, "")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}

	if strings.HasSuffix(from.Uri, "/") != strings.HasSuffix(to.Uri, "/") {
		err := fmt.Errorf("FROM and TO should both end with path separator or not")
		l.Error(err, "")
		sync.Status.Phase = juicefsiov1.SyncPhaseFailed
		sync.Status.Reason = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, sync)
	}

	if sync.Status.Phase == juicefsiov1.SyncPhasePending || sync.Status.Phase == "" {
		sync.Status.Phase = juicefsiov1.SyncPhasePreparing
		sync.Status.PreparingAt = &metav1.Time{Time: time.Now()}
		if err := r.updateStatus(ctx, sync); err != nil {
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhasePreparing {
		if sync.Spec.PreparingTimeoutSeconds != nil {
			PreparingTimeoutSeconds := *sync.Spec.PreparingTimeoutSeconds
			if sync.Status.PreparingAt != nil && time.Since(sync.Status.PreparingAt.Time) > time.Duration(PreparingTimeoutSeconds)*time.Second {
				l.Info("sync preparing phase timed out", "timeout", PreparingTimeoutSeconds)
				sync.Status.Phase = juicefsiov1.SyncPhaseFailed
				sync.Status.Reason = fmt.Sprintf("Preparing phase timed out after %d seconds", PreparingTimeoutSeconds)
				return ctrl.Result{}, r.updateStatus(ctx, sync)
			}
		}

		// prepare secrets
		if err := r.prepareSyncSecrets(ctx, sync); err != nil {
			l.Error(err, "failed to prepare sync secrets")
			return ctrl.Result{}, err
		}

		builder := builder.NewSyncPodBuilder(sync, from, to)
		l.Info("start to prepare sync worker pods", "replicas", sync.Spec.Replicas)
		if err := r.prepareWorkerPod(ctx, sync, builder); err != nil {
			l.Error(err, "failed to prepare worker pod")
			return ctrl.Result{}, err
		}
		l.Info("prepare worker pod done", "replicas", sync.Spec.Replicas)

		l.Info("start to prepare sync manager pod")
		// prepare manager pod
		if err := r.prepareManagerPod(ctx, sync, builder); err != nil {
			l.Error(err, "failed to prepare manager pod")
			return ctrl.Result{}, err
		}
		l.Info("prepare manager pod done, ready to sync")

		sync.Status.StartAt = &metav1.Time{Time: time.Now()}
		sync.Status.Phase = juicefsiov1.SyncPhaseProgressing
		if err := r.updateStatus(ctx, sync); err != nil {
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseCompleted {
		// delete worker pod
		if err := r.deleteWorkerPods(ctx, sync, true); err != nil {
			l.Error(err, "failed to delete worker pods")
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

	if sync.Status.Phase == juicefsiov1.SyncPhaseFailed {
		if err := r.deleteWorkerPods(ctx, sync, true); err != nil {
			l.Error(err, "failed to delete worker pods")
			return ctrl.Result{}, err
		}
	}

	if sync.Status.Phase == juicefsiov1.SyncPhaseProgressing {
		// get manager pod
		managerPod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: common.GenSyncManagerName(sync.Name)}, managerPod); err != nil {
			if apierrors.IsNotFound(err) {
				sync.Status.Phase = juicefsiov1.SyncPhaseFailed
				sync.Status.Reason = "manager pod not found"
				return ctrl.Result{}, r.updateStatus(ctx, sync)
			}
			l.Error(err, "failed to get manager pod")
			return ctrl.Result{}, err
		}

		// delete worker completed pod
		if err := r.deleteWorkerPods(ctx, sync, false); err != nil {
			l.Error(err, "failed to delete worker pods")
			return ctrl.Result{}, err
		}

		status, err := r.calculateSyncStats(ctx, sync, managerPod)
		if !reflect.DeepEqual(sync.Status, status) {
			sync.Status = status
			if err := r.updateStatus(ctx, sync); err != nil {
				return ctrl.Result{}, err
			}
		}
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SyncReconciler) calculateSyncStats(ctx context.Context, sync *juicefsiov1.Sync, managerPod *corev1.Pod) (juicefsiov1.SyncStatus, error) {
	l := log.FromContext(ctx)
	status := sync.Status
	if managerPod.Status.Phase == corev1.PodSucceeded || managerPod.Status.Phase == corev1.PodFailed {
		if managerPod.Status.Phase == corev1.PodFailed {
			status.Phase = juicefsiov1.SyncPhaseFailed
		} else {
			status.Phase = juicefsiov1.SyncPhaseCompleted
		}
		status.CompletedAt = &metav1.Time{Time: time.Now()}
		finishLog, err := utils.LogPod(ctx, sync.Namespace, common.GenSyncManagerName(sync.Name), common.SyncNamePrefix, 20)
		if err != nil {
			status.Reason = "failed to get manager pod last logs\nerror: " + err.Error()
			l.Error(err, "failed to get manager pod last logs")
			return status, err
		}
		if len(finishLog) > 0 {
			status.FinishLog = finishLog
		}
		statsMap, err := utils.ParseLog(status.FinishLog)
		if err != nil {
			status.Reason = "failed to parse log\nerror: " + err.Error()
			l.Error(err, "failed to parse log")
		} else {
			stats := juicefsiov1.SyncStats{}
			fieldMappings := []struct {
				key    string
				target *int64
			}{
				{"found", &stats.Handled},
				{"copied", &stats.Copied},
				{"copied_bytes", &stats.CopiedBytes},
				{"failed", &stats.Failed},
				{"skipped", &stats.Skipped},
				{"skipped_bytes", &stats.SkippedBytes},
				{"checked", &stats.Checked},
				{"checked_bytes", &stats.CheckedBytes},
				{"lost", &stats.Lost},
				{"deleted", &stats.Deleted},
				{"excluded", &stats.Excluded},
				{"exclude_bytes", &stats.ExcludeBytes},
				{"extra", &stats.Extra},
				{"extra_bytes", &stats.ExtraBytes},
			}
			for _, mapping := range fieldMappings {
				if value, ok := statsMap[mapping.key]; ok {
					*mapping.target = value
				}
			}
			if stats.Lost > 0 || stats.Failed > 0 {
				status.Phase = juicefsiov1.SyncPhaseFailed
			}
			status.Progress = utils.CalculateProgress(stats.Handled-stats.Failed-stats.Lost, stats.Handled)
			stats.Pending = 0
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
			l.Error(err, "failed to fetch metrics from manager pod")
			return status, nil
		}
		if _, ok := metrics["juicefs_sync_scanned"]; !ok {
			l.Info("metrics does not include the required metric, skip update stats", "metrics", metrics)
			return status, nil
		}
		stats := juicefsiov1.SyncStats{
			Handled:      int64(metrics["juicefs_sync_handled"]),
			Copied:       int64(metrics["juicefs_sync_copied"]),
			Failed:       int64(metrics["juicefs_sync_failed"]),
			Skipped:      int64(metrics["juicefs_sync_skipped"]),
			Checked:      int64(metrics["juicefs_sync_checked"]),
			CopiedBytes:  int64(metrics["juicefs_sync_copied_bytes"]),
			Scanned:      int64(metrics["juicefs_sync_scanned"]),
			Pending:      int64(metrics["juicefs_sync_pending"]),
			Deleted:      int64(metrics["juicefs_sync_deleted"]),
			Extra:        int64(metrics["juicefs_sync_extra"]),
			Excluded:     int64(metrics["juicefs_sync_excluded"]),
			CheckedBytes: int64(metrics["juicefs_sync_checked_bytes"]),
			SkippedBytes: int64(metrics["juicefs_sync_skipped_bytes"]),
			ExtraBytes:   int64(metrics["juicefs_sync_extra_bytes"]),
			ExcludeBytes: int64(metrics["juicefs_sync_exclude_bytes"]),
			LastUpdated:  &metav1.Time{Time: time.Now()},
		}
		status.Progress = utils.CalculateProgress(stats.Handled, stats.Scanned)
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
		common.LabelSync:    utils.TruncateLabelValue(sync.Name),
		common.LabelAppType: common.LabelSyncWorkerValue,
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, labelSelector); err != nil {
		return err
	}

	if len(pods.Items) != int(*sync.Spec.Replicas)-1 {
		workerPods := builder.NewWorkerPods()
		existPods := lo.KeyBy(pods.Items, func(pod corev1.Pod) string {
			return pod.Name
		})
		for _, pod := range workerPods {
			if _, ok := existPods[pod.Name]; ok {
				continue
			}
			err := r.Create(ctx, &pod)
			if err != nil && !apierrors.IsAlreadyExists(err) {
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
			pods = &corev1.PodList{}
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
				// If the pod is in a succeeded phase here, may be the sync has been completed
				// and the status update error by conflict, we can ignore it.
				if utils.IsPodReady(pod) || pod.Status.Phase == corev1.PodSucceeded {
					ips = append(ips, pod.Status.PodIP)
				} else {
					log.Info("worker pod not ready", "pod", pod.Name, "status", pod.Status.Phase, "reason", utils.PodNotReadyReason(pod))
					break
				}
			}
			if len(ips) == int(*sync.Spec.Replicas)-1 {
				log.Info("sync worker pod ready", "ips", ips)
				builder.UpdateWorkerIPs(ips)
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (r *SyncReconciler) deleteWorkerPods(ctx context.Context, sync *juicefsiov1.Sync, all bool) error {
	labelSelector := client.MatchingLabels{
		common.LabelSync:    utils.TruncateLabelValue(sync.Name),
		common.LabelAppType: common.LabelSyncWorkerValue,
	}
	var fieldSelector client.MatchingFields
	if !all {
		fieldSelector = client.MatchingFields{
			"status.phase": string(corev1.PodSucceeded),
		}
	}
	return client.IgnoreNotFound(
		r.DeleteAllOf(ctx, &corev1.Pod{},
			client.InNamespace(sync.Namespace),
			labelSelector,
			fieldSelector,
		))
}

func (r *SyncReconciler) prepareManagerPod(ctx context.Context, sync *juicefsiov1.Sync, builder *builder.SyncPodBuilder) error {
	managerPod := builder.NewManagerPod()
	err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: managerPod.Name}, &corev1.Pod{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, managerPod); err != nil {
			return err
		}
	}
	// waiting for manager pod ready
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for manager pod ready")
		default:
			err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: managerPod.Name}, managerPod)
			if err != nil {
				if apierrors.IsNotFound(err) {
					time.Sleep(1 * time.Second)
					continue
				}
				return err
			}
			if utils.IsPodReady(*managerPod) {
				log.FromContext(ctx).Info("sync manager pod ready")
				return nil
			}
			// It may have failed/successed immediately after starting, also returns success at this time.
			if managerPod.Status.Phase == corev1.PodFailed || managerPod.Status.Phase == corev1.PodSucceeded {
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (r *SyncReconciler) updateStatus(ctx context.Context, sync *juicefsiov1.Sync) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newSync := &juicefsiov1.Sync{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: sync.Name}, newSync); err != nil {
			return err
		}
		newSync.Status = sync.Status
		return r.Status().Update(ctx, newSync)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *SyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.Sync{}).
		Owns(&corev1.Pod{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxSyncConcurrentReconciles,
		}).
		Named("sync").
		Complete(r)
}
