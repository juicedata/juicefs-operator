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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/builder"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/utils"
)

// WarmUpReconciler reconciles a WarmUp object
type WarmUpReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=warmups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=warmups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=warmups/finalizers,verbs=update

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
		logger.Error(err, "unable to fetch WarmUp")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	handler := r.getWarmupHandler(wu.Spec.Policy.Type)
	if handler == nil {
		logger.Error(fmt.Errorf("unsupported policy type %s", wu.Spec.Policy.Type), "unable to get WarmUp handler")
		return ctrl.Result{}, nil
	}

	if wu.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if err := handler.sync(ctx, wu); err != nil {
		logger.Error(err, "unable to sync WarmUp")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WarmUpReconciler) getWarmupHandler(policyType juicefsiov1.PolicyType) warmUpHandler {
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
	sync(ctx context.Context, wu *juicefsiov1.WarmUp) (err error)
}

type onceHandler struct {
	client.Client
}

var _ warmUpHandler = &onceHandler{}

func (o *onceHandler) sync(ctx context.Context, wu *juicefsiov1.WarmUp) (err error) {
	l := log.FromContext(ctx)
	jobName := common.GenJobName(wu.Name)
	var job batchv1.Job
	if err := o.Get(ctx, client.ObjectKey{Namespace: wu.Namespace, Name: jobName}, &job); err != nil {
		if utils.IsNotFound(err) {
			// create a new job
			podList := &corev1.PodList{}
			if err := o.List(ctx, podList, client.MatchingLabels{common.LabelCacheGroup: wu.Spec.CacheGroupName}); err != nil {
				l.Error(err, "list pod error", "cache group", wu.Spec.CacheGroupName)
				return err
			}
			if len(podList.Items) == 0 {
				return fmt.Errorf("no worker found for cache group %s", wu.Spec.CacheGroupName)
			}

			jobBuilder := builder.NewJobBuilder(wu, &podList.Items[0])
			newJob := jobBuilder.NewWarmupJob()
			err = o.Create(ctx, newJob)
			if err != nil {
				return err
			}
			job = *newJob
		} else {
			l.Error(err, "get job error", "job", jobName)
			return err
		}
	}

	newStatus := o.calculateStatus(ctx, &job)
	if !reflect.DeepEqual(wu.Status, newStatus) {
		wu.Status = *newStatus
		return utils.IgnoreConflict(o.Status().Update(ctx, wu))
	}

	return nil
}

func (o *onceHandler) calculateStatus(ctx context.Context, job *batchv1.Job) *juicefsiov1.WarmUpStatus {
	l := log.FromContext(ctx)
	status := &juicefsiov1.WarmUpStatus{}
	if job == nil {
		status.Phase = juicefsiov1.WarmUpPhasePending
		return status
	}
	finishedJobCondition := utils.GetFinishedJobCondition(job)
	if finishedJobCondition == nil {
		l.Info("WarmUp job still running", "namespace", job.Namespace, "jobName", job.Name)
		status.Phase = juicefsiov1.WarmUpPhaseRunning
		return status
	}

	status.Conditions = append(status.Conditions,
		juicefsiov1.Condition{
			Type:               string(finishedJobCondition.Type),
			Status:             string(finishedJobCondition.Status),
			LastTransitionTime: finishedJobCondition.LastTransitionTime,
		},
	)
	if finishedJobCondition.Type == batchv1.JobComplete {
		status.Phase = juicefsiov1.WarmUpPhaseComplete
	} else {
		status.Phase = juicefsiov1.WarmUpPhaseFailed
	}
	status.Duration = utils.CalculateDuration(job.CreationTimestamp.Time, finishedJobCondition.LastTransitionTime.Time)
	return status
}

type cronHandler struct {
	client.Client
}

var _ warmUpHandler = &cronHandler{}

func (c *cronHandler) sync(ctx context.Context, wu *juicefsiov1.WarmUp) (err error) {
	//TODO implement me
	panic("implement me")
}

// SetupWithManager sets up the controller with the Manager.
func (r *WarmUpReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.WarmUp{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
