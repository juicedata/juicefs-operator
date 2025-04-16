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
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"maps"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CronSyncReconciler reconciles a CronSync object
type CronSyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=cronsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=cronsyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=cronsyncs/finalizers,verbs=update
// +kubebuilder:rbac:groups=juicefs.io,resources=syncs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CronSync object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *CronSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var cronSync juicefsiov1.CronSync
	if err := r.Get(ctx, req.NamespacedName, &cronSync); err != nil {
		log.Error(err, "unable to fetch CronSync")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if we need to suspend execution
	if cronSync.Spec.Suspend != nil && *cronSync.Spec.Suspend {
		log.Info("CronSync suspended, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	cronSchedule, err := cron.ParseStandard(cronSync.Spec.Schedule)
	if err != nil {
		log.Error(err, "unable to parse schedule", "schedule", cronSync.Spec.Schedule)
		return ctrl.Result{}, err
	}
	now := time.Now()
	nextScheduleTime := cronSchedule.Next(now)
	duration := nextScheduleTime.Sub(now)

	// first time reconcile
	if cronSync.Status.LastScheduleTime == nil {
		cronSync.Status.LastScheduleTime = &metav1.Time{Time: now}
		cronSync.Status.NextScheduleTime = &metav1.Time{Time: nextScheduleTime}
		if err := r.Status().Update(ctx, &cronSync); err != nil {
			log.Error(err, "unable to update CronSync status")
			return ctrl.Result{}, err
		}
	}

	historyJobs, err := r.ListCronSyncJobs(ctx, &cronSync)
	if err != nil {
		log.Error(err, "unable to list sync jobs")
		return ctrl.Result{}, err
	}

	if err := r.SyncHistory(ctx, &cronSync, historyJobs); err != nil {
		log.Error(err, "unable to sync history")
		return ctrl.Result{}, err
	}

	lastScheduledTime := cronSync.Status.LastScheduleTime.Time
	// if this call is caused by an update (next schedule is earlier than now), ignore
	if cronSchedule.Next(lastScheduledTime).After(now) {
		log.V(1).Info("Scheduled next reconcile", "at", nextScheduleTime.Format(time.RFC3339))
		return ctrl.Result{RequeueAfter: duration}, r.Status().Update(ctx, &cronSync)
	}

	runningJobs := lo.Filter(historyJobs, func(job juicefsiov1.Sync, _ int) bool {
		return job.Status.Phase == juicefsiov1.SyncPhasePending ||
			job.Status.Phase == juicefsiov1.SyncPhasePreparing ||
			job.Status.Phase == juicefsiov1.SyncPhaseProgressing
	})

	switch cronSync.Spec.ConcurrencyPolicy {
	case batchv1.AllowConcurrent:
	// do nothing
	case batchv1.ForbidConcurrent:
		if len(runningJobs) > 0 {
			log.V(1).Info("CronSync is running, skipping reconciliation")
			return ctrl.Result{}, nil
		}
	case batchv1.ReplaceConcurrent:
		if len(runningJobs) > 0 {
			for _, job := range runningJobs {
				log.V(1).Info("Deleting old sync job", "job", job.Name)
				if err := r.Delete(ctx, &job); err != nil {
					log.Error(err, "unable to delete old sync job", "job", job.Name)
					return ctrl.Result{}, err
				}
			}
		}
	}

	syncJob := newSyncJob(&cronSync, now)
	log.Info("Triggering sync job", "job", syncJob)
	if err := r.Create(ctx, syncJob); err != nil {
		log.Error(err, "unable to create sync job", "job", syncJob)
		return ctrl.Result{}, r.Status().Update(ctx, &cronSync)
	}
	log.Info("Sync job created", "job", syncJob.Name)

	cronSync.Status.LastScheduleTime = &metav1.Time{Time: now}
	cronSync.Status.NextScheduleTime = &metav1.Time{Time: nextScheduleTime}
	if err := r.Status().Update(ctx, &cronSync); err != nil {
		log.Error(err, "unable to update CronSync status")
		return ctrl.Result{}, err
	}

	log.V(1).Info("Scheduled next reconcile", "at", nextScheduleTime.Format(time.RFC3339))
	return ctrl.Result{RequeueAfter: duration}, nil

}

func newSyncJob(cronSync *juicefsiov1.CronSync, now time.Time) *juicefsiov1.Sync {
	job := &juicefsiov1.Sync{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenCronSyncJobName(cronSync.Name, now),
			Namespace:   cronSync.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				common.LabelManagedBy: common.LabelManagedByValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         cronSync.APIVersion,
					Kind:               cronSync.Kind,
					Name:               cronSync.Name,
					UID:                cronSync.UID,
					Controller:         lo.ToPtr(true),
					BlockOwnerDeletion: lo.ToPtr(true),
				},
			},
		},
		Spec: cronSync.Spec.SyncTemplate.Spec,
	}
	maps.Copy(job.Labels, cronSync.Spec.SyncTemplate.Labels)
	maps.Copy(job.Annotations, cronSync.Spec.SyncTemplate.Annotations)
	job.Labels[common.LabelCronSync] = utils.TruncateLabelValue(cronSync.Name)
	return job
}

func (r *CronSyncReconciler) ListCronSyncJobs(ctx context.Context, cronSync *juicefsiov1.CronSync) ([]juicefsiov1.Sync, error) {
	var syncList juicefsiov1.SyncList
	labelSelector := client.MatchingLabels{
		common.LabelCronSync: utils.TruncateLabelValue(cronSync.Name),
	}
	if err := r.List(ctx, &syncList, client.InNamespace(cronSync.Namespace), labelSelector); err != nil {
		return nil, err
	}
	return syncList.Items, nil
}

func (r *CronSyncReconciler) SyncHistory(ctx context.Context, cronSync *juicefsiov1.CronSync, syncJobs []juicefsiov1.Sync) error {
	log := log.FromContext(ctx)
	phaseJobMap := lo.GroupBy(syncJobs, func(syncJob juicefsiov1.Sync) string {
		return string(syncJob.Status.Phase)
	})

	// Update LastSuccessfulTime if there are completed jobs
	if len(phaseJobMap[string(juicefsiov1.SyncPhaseCompleted)]) > 0 {
		completedJobs := phaseJobMap[string(juicefsiov1.SyncPhaseCompleted)]
		sort.Slice(completedJobs, func(i, j int) bool {
			return completedJobs[i].Status.CompletedAt.Time.After(completedJobs[j].Status.CompletedAt.Time)
		})
		cronSync.Status.LastSuccessfulTime = &metav1.Time{Time: completedJobs[0].Status.CompletedAt.Time}
	}

	// Delete old sync jobs
	checkAndDelete := func(phase string, limit int) error {
		jobs := phaseJobMap[phase]
		if len(jobs) < limit {
			return nil
		}
		sort.Slice(jobs, func(i, j int) bool {
			return jobs[i].CreationTimestamp.Time.Before(jobs[j].CreationTimestamp.Time)
		})
		for _, job := range jobs[:len(jobs)-limit] {
			log.V(1).Info("Deleting old sync job", "job", job.Name)
			if err := r.Delete(ctx, &job); err != nil {
				log.Error(err, "unable to delete old sync job", "job", job.Name)
				return err
			}
		}
		return nil
	}
	if err := checkAndDelete(string(juicefsiov1.SyncPhaseCompleted), int(*cronSync.Spec.SuccessfulJobsHistoryLimit)); err != nil {
		return err
	}
	if err := checkAndDelete(string(juicefsiov1.SyncPhaseFailed), int(*cronSync.Spec.FailedJobsHistoryLimit)); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CronSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.CronSync{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxSyncConcurrentReconciles,
		}).
		Owns(&juicefsiov1.Sync{}).
		Named("cronsync").
		Complete(r)
}
