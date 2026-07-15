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
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

// GatewayReconciler reconciles a Gateway object
type GatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=gateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=gateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.V(1).Info("gateway reconcile")

	gw := &juicefsiov1.Gateway{}
	if err := r.Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if !gw.GetDeletionTimestamp().IsZero() {
		l.V(1).Info("gateway is being deleted")
		return ctrl.Result{}, nil
	}

	// Resolve the secret for EE
	var secret *corev1.Secret
	if gw.Spec.SecretRef != nil {
		secret = &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: gw.Spec.SecretRef.Name}, secret); err != nil {
			l.Error(err, "failed to get gateway secret", "secret", gw.Spec.SecretRef.Name)
			return ctrl.Result{}, err
		}
		if err := utils.ValidateSecret(secret); err != nil {
			l.Error(err, "gateway secret validation failed")
			return ctrl.Result{}, err
		}
	}

	gwBuilder := builder.NewGatewayBuilder(gw, secret)

	if err := r.syncDeployment(ctx, gw, gwBuilder); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.syncService(ctx, gw, gwBuilder); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.syncStatus(ctx, gw); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) syncDeployment(ctx context.Context, gw *juicefsiov1.Gateway, gwBuilder *builder.GatewayBuilder) error {
	l := log.FromContext(ctx)
	desired := gwBuilder.NewDeployment(ctx)

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: desired.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("creating gateway deployment", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update if spec changed (use annotation hash to detect changes).
	if existing.Annotations[common.LabelWorkerHash] != desired.Annotations[common.LabelWorkerHash] {
		l.Info("updating gateway deployment", "name", desired.Name)
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[common.LabelWorkerHash] = desired.Annotations[common.LabelWorkerHash]
		return utils.IgnoreConflict(r.Update(ctx, existing))
	}
	return nil
}

func (r *GatewayReconciler) syncService(ctx context.Context, gw *juicefsiov1.Gateway, gwBuilder *builder.GatewayBuilder) error {
	l := log.FromContext(ctx)
	desired := gwBuilder.NewService()

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: desired.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("creating gateway service", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update if service spec changed (type or ports).
	if existing.Spec.Type != desired.Spec.Type ||
		!reflect.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) ||
		!reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		l.Info("updating gateway service", "name", desired.Name)
		existing.Spec.Type = desired.Spec.Type
		existing.Spec.Ports = desired.Spec.Ports
		existing.Spec.Selector = desired.Spec.Selector
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations
		return utils.IgnoreConflict(r.Update(ctx, existing))
	}
	return nil
}

func (r *GatewayReconciler) syncStatus(ctx context.Context, gw *juicefsiov1.Gateway) error {
	deployName := builder.GenGatewayName(gw.Name)
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: deployName}, deploy); err != nil {
		return client.IgnoreNotFound(err)
	}

	phase := juicefsiov1.GatewayPhaseProgressing
	if deploy.Status.Replicas > 0 && deploy.Status.ReadyReplicas == deploy.Status.Replicas {
		phase = juicefsiov1.GatewayPhaseReady
	}

	newStatus := juicefsiov1.GatewayStatus{
		Phase:         phase,
		ReadyReplicas: deploy.Status.ReadyReplicas,
		Replicas:      deploy.Status.Replicas,
	}

	if !reflect.DeepEqual(gw.Status, newStatus) {
		gw.Status = newStatus
		return utils.IgnoreConflict(r.Status().Update(ctx, gw))
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxGatewayConcurrentReconciles,
		}).
		Complete(r)
}
