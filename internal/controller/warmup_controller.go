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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

// WarmUpReconciler reconciles a WarmUp object
type WarmUpReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=warmups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=warmups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=warmups/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;delete;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;create;delete;watch;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the WarmUp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *WarmUpReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	wu := &juicefsiov1.WarmUp{}
	if err := r.Get(ctx, req.NamespacedName, wu); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch WarmUp")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("reconcile WarmUp", "warmup", wu.Name)

	cg := &juicefsiov1.CacheGroup{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: wu.Namespace,
		Name:      wu.Spec.CacheGroupName,
	}, cg); err != nil {
		// try to get via status
		cgList := &juicefsiov1.CacheGroupList{}
		if err := r.List(ctx, cgList, client.MatchingFields{
			"status.cacheGroup": wu.Spec.CacheGroupName,
		}); err != nil {
			logger.Error(err, "unable to fetch CacheGroup", "cache group", wu.Spec.CacheGroupName)
			return ctrl.Result{}, err
		}
		if len(cgList.Items) > 0 {
			cg = &cgList.Items[0]
		} else {
			logger.Error(err, "unable to fetch CacheGroup", "cache group", wu.Spec.CacheGroupName)
			return ctrl.Result{}, err
		}
	}

	if wu.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	handler := r.getWarmUpHandler(wu.Spec.Policy.Type)
	if handler == nil {
		logger.Error(fmt.Errorf("unsupported policy type %s", wu.Spec.Policy.Type), "unable to get WarmUp handler")
		return ctrl.Result{}, nil
	}

	if result, err := handler.sync(ctx, wu, cg.Name); err != nil {
		logger.Error(err, "unable to sync WarmUp")
		return result, err
	} else {
		return result, nil
	}
}

func (r *WarmUpReconciler) getWarmUpHandler(policyType juicefsiov1.PolicyType) warmUpHandler {
	switch policyType {
	case "":
		fallthrough
	case juicefsiov1.PolicyTypeOnce:
		return &onceHandler{r.Client}
	case juicefsiov1.PolicyTypeCron:
		return &cronHandler{r.Client}
	}
	return nil
}

type warmUpHandler interface {
	sync(ctx context.Context, wu *juicefsiov1.WarmUp, cgName string) (result reconcile.Result, err error)
}

type onceHandler struct {
	client.Client
}

var _ warmUpHandler = &onceHandler{}

func (o *onceHandler) sync(ctx context.Context, wu *juicefsiov1.WarmUp, cgName string) (result reconcile.Result, err error) {
	l := log.FromContext(ctx)
	if wu.Status.Phase == juicefsiov1.WarmUpPhaseFailed {
		return reconcile.Result{}, nil
	}
	if wu.Status.Phase == juicefsiov1.WarmUpPhaseComplete {
		if wu.Spec.TTLSecondsAfterFinished != nil {
			completedAt := wu.Status.LastCompleteTime
			if completedAt == nil {
				return ctrl.Result{}, nil
			}
			since := float64(*wu.Spec.TTLSecondsAfterFinished) - time.Since(completedAt.Time).Seconds()
			if since <= 0 {
				l.Info("warmup ttl is expired, deleted")
				if err := o.Delete(ctx, wu); err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
			}
			return ctrl.Result{RequeueAfter: time.Second*time.Duration(since) + 1}, nil
		}
		return reconcile.Result{}, nil
	}
	jobName := utils.GenJobName(wu.Name)
	var job batchv1.Job
	if err := o.Get(ctx, client.ObjectKey{Namespace: wu.Namespace, Name: jobName}, &job); err != nil {
		if utils.IsNotFound(err) {
			// create a new job
			podList := &corev1.PodList{}
			if err := o.List(ctx, podList, client.MatchingLabels{common.LabelCacheGroup: utils.TruncateLabelValue(cgName)}, &client.ListOptions{
				Limit: 1,
			}); err != nil {
				l.Error(err, "list pod error", "cache group", cgName)
				return reconcile.Result{}, err
			}
			if len(podList.Items) == 0 {
				return reconcile.Result{}, fmt.Errorf("no worker found for cache group %s", cgName)
			}
			l.Info("get worker of cacheGroup for warmup", "cacheGroup", cgName, "worker", podList.Items[0].Name)

			jobBuilder := builder.NewJobBuilder(wu, &podList.Items[0])
			newJob := jobBuilder.NewWarmUpJob()
			l.Info("create warmup job", "job", newJob.Name)
			err = o.Create(ctx, newJob)
			if err != nil {
				l.Error(err, "create job error", "job", newJob.Name)
				return reconcile.Result{}, err
			}
			job = *newJob
		} else {
			l.Error(err, "get job error", "job", jobName)
			return reconcile.Result{}, err
		}
	}

	newStatus := o.calculateStatus(ctx, &job, cgName)
	if utils.WarmupSupportStats(job.Spec.Template.Spec.Containers[0].Image) && wu.Status.Phase == juicefsiov1.WarmUpPhaseRunning {
		result = reconcile.Result{
			Requeue:      true,
			RequeueAfter: common.UpdateWarmupStatsInterval,
		}
	}
	if !reflect.DeepEqual(wu.Status, newStatus) {
		wu.Status = newStatus
		return result, utils.IgnoreConflict(o.Status().Update(ctx, wu))
	}
	return result, nil
}

func (o *onceHandler) calculateStatus(ctx context.Context, job *batchv1.Job, cgName string) juicefsiov1.WarmUpStatus {
	l := log.FromContext(ctx)
	status := juicefsiov1.WarmUpStatus{
		CacheGroup: cgName,
	}
	if job == nil {
		status.Phase = juicefsiov1.WarmUpPhasePending
		return status
	}
	finishedJobCondition := utils.GetFinishedJobCondition(job)
	var err error
	if finishedJobCondition == nil {
		l.Info("WarmUp job still running", "namespace", job.Namespace, "jobName", job.Name)
		status.Phase = juicefsiov1.WarmUpPhaseRunning
		status.Stats, _, err = utils.GetWarmupJobStats(ctx, job)
		if err != nil {
			l.Info("failed to get warmup job stats", "job", job.Name, "err", err.Error())
		}
		status.Progress = utils.CalculateWarmupProgress(status.Stats)
		return status
	}

	status.Stats, status.FinishLog, err = utils.GetWarmupJobStats(ctx, job)
	if err != nil {
		l.Info("failed to get warmup job stats", "job", job.Name, "err", err.Error())
	}
	status.Progress = utils.CalculateWarmupProgress(status.Stats)
	status.Conditions = append(status.Conditions,
		juicefsiov1.Condition{
			Type:               string(finishedJobCondition.Type),
			Status:             string(finishedJobCondition.Status),
			LastTransitionTime: finishedJobCondition.LastTransitionTime,
		},
	)
	status.LastScheduleTime = job.Status.StartTime
	if finishedJobCondition.Type == batchv1.JobComplete {
		status.Phase = juicefsiov1.WarmUpPhaseComplete
		status.LastCompleteTime = job.Status.CompletionTime
	} else {
		status.Phase = juicefsiov1.WarmUpPhaseFailed
	}
	if status.Stats.FailedBlocks > 0 {
		status.Phase = juicefsiov1.WarmUpPhaseFailed
	}
	status.Duration = utils.CalculateDuration(job.CreationTimestamp.Time, finishedJobCondition.LastTransitionTime.Time)
	return status
}

type cronHandler struct {
	client.Client
}

var _ warmUpHandler = &cronHandler{}

func (c *cronHandler) sync(ctx context.Context, wu *juicefsiov1.WarmUp, cgName string) (result reconcile.Result, err error) {
	l := log.FromContext(ctx)
	var cronjob batchv1.CronJob
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingLabels{common.LabelCacheGroup: utils.TruncateLabelValue(cgName)}, &client.ListOptions{Limit: 1}); err != nil {
		l.Error(err, "list pod error", "cache group", cgName)
		return reconcile.Result{}, err
	}
	if len(podList.Items) == 0 {
		return reconcile.Result{}, fmt.Errorf("no worker found for cache group %s", cgName)
	}
	l.Info("get worker of cacheGroup for warmup", "cacheGroup", cgName, "worker", podList.Items[0].Name)
	cronjobBuilder := builder.NewJobBuilder(wu, &podList.Items[0])
	newCronJob := cronjobBuilder.NewWarmUpCronJob()

	if err := c.Get(ctx, client.ObjectKey{Namespace: wu.Namespace, Name: newCronJob.Name}, &cronjob); err != nil {
		if utils.IsNotFound(err) {
			l.Info("create warmup cronjob", "cronjob", newCronJob.Name)
			err = c.Create(ctx, newCronJob)
			if err != nil {
				l.Error(err, "create cronjob error", "cronjob", newCronJob.Name)
				return reconcile.Result{}, err
			}
			cronjob = *newCronJob
		} else {
			l.Error(err, "get cronjob error", "cronjob", newCronJob.Name)
			return reconcile.Result{}, err
		}
	} else {
		if newCronJob.Annotations[common.LabelWorkerHash] != cronjob.Annotations[common.LabelWorkerHash] {
			l.Info("update warmup cronjob", "cronjob", newCronJob.Name)
			if err := c.Update(ctx, newCronJob); err != nil {
				return reconcile.Result{}, utils.IgnoreConflict(err)
			}
		}
		return reconcile.Result{}, err
	}
	newStatus := c.calculateStatus(&cronjob)
	if !reflect.DeepEqual(wu.Status, newStatus) {
		wu.Status = *newStatus
		return reconcile.Result{}, utils.IgnoreConflict(c.Status().Update(ctx, wu))
	}
	return reconcile.Result{}, nil
}

func (o *cronHandler) calculateStatus(crobjob *batchv1.CronJob) *juicefsiov1.WarmUpStatus {
	status := &juicefsiov1.WarmUpStatus{}
	if crobjob == nil {
		status.Phase = juicefsiov1.WarmUpPhasePending
		return status
	}
	status.Phase = juicefsiov1.WarmUpPhaseRunning
	status.LastScheduleTime = crobjob.Status.LastScheduleTime
	status.LastCompleteTime = crobjob.Status.LastSuccessfulTime
	return status
}

// SetupWithManager sets up the controller with the Manager.
func (r *WarmUpReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &juicefsiov1.CacheGroup{}, "status.cacheGroup", func(o client.Object) []string {
		cg := o.(*juicefsiov1.CacheGroup)
		return []string{cg.Status.CacheGroup}
	}); err != nil {
		return err
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.WarmUp{}).
		Owns(&batchv1.Job{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxWarmupConcurrentReconciles,
		})

	if isCronJobAPIAvailable(mgr) {
		controllerBuilder = controllerBuilder.Owns(&batchv1.CronJob{})
	} else {
		log.Log.Info("CronJob API is not available in this Kubernetes cluster, scheduled warmup functionality will be disabled. Only manual warmup jobs will be supported.")
	}

	return controllerBuilder.Complete(r)
}

// isCronJobAPIAvailable checks if the CronJob API is available in the cluster
func isCronJobAPIAvailable(mgr ctrl.Manager) bool {
	mapper := mgr.GetRESTMapper()
	gvk := batchv1.SchemeGroupVersion.WithKind("CronJob")
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}
