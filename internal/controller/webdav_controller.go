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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/builder"
	"github.com/juicedata/juicefs-operator/pkg/common"
)

// WebDAVReconciler reconciles a WebDAV object
type WebDAVReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=juicefs.io,resources=webdavs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=juicefs.io,resources=webdavs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=juicefs.io,resources=webdavs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles a WebDAV object.
func (r *WebDAVReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.V(1).Info("webdav reconcile")

	webdav := &juicefsiov1.WebDAV{}
	if err := r.Get(ctx, req.NamespacedName, webdav); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !webdav.GetDeletionTimestamp().IsZero() {
		l.V(1).Info("webdav is being deleted")
		return ctrl.Result{}, nil
	}

	// Fetch the secret
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: webdav.Namespace,
		Name:      webdav.Spec.SecretRef.Name,
	}, secret); err != nil {
		l.Error(err, "failed to get secret", "secret", webdav.Spec.SecretRef.Name)
		return ctrl.Result{}, err
	}

	b := builder.NewWebDAVBuilder(webdav, secret)

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, webdav, b); err != nil {
		l.Error(err, "failed to reconcile deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, webdav, b); err != nil {
		l.Error(err, "failed to reconcile service")
		return ctrl.Result{}, err
	}

	// Sync status
	if err := r.syncStatus(ctx, webdav); err != nil {
		l.Error(err, "failed to sync status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WebDAVReconciler) reconcileDeployment(ctx context.Context, webdav *juicefsiov1.WebDAV, b *builder.WebDAVBuilder) error {
	desired := b.NewWebDAVDeployment(ctx)
	existing := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Namespace: webdav.Namespace, Name: desired.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update the deployment if the spec has changed
	updated := existing.DeepCopy()
	updated.Spec.Replicas = desired.Spec.Replicas
	updated.Spec.Template = desired.Spec.Template
	updated.Labels = desired.Labels

	if !reflect.DeepEqual(existing.Spec, updated.Spec) || !reflect.DeepEqual(existing.Labels, updated.Labels) {
		return r.Update(ctx, updated)
	}
	return nil
}

func (r *WebDAVReconciler) reconcileService(ctx context.Context, webdav *juicefsiov1.WebDAV, b *builder.WebDAVBuilder) error {
	desired := b.NewWebDAVService()
	existing := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Namespace: webdav.Namespace, Name: desired.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update the service if the spec has changed
	updated := existing.DeepCopy()
	updated.Spec.Ports = desired.Spec.Ports
	updated.Spec.Type = desired.Spec.Type
	updated.Spec.Selector = desired.Spec.Selector
	updated.Labels = desired.Labels

	if !reflect.DeepEqual(existing.Spec.Ports, updated.Spec.Ports) ||
		existing.Spec.Type != updated.Spec.Type ||
		!reflect.DeepEqual(existing.Spec.Selector, updated.Spec.Selector) ||
		!reflect.DeepEqual(existing.Labels, updated.Labels) {
		return r.Update(ctx, updated)
	}
	return nil
}

func (r *WebDAVReconciler) syncStatus(ctx context.Context, webdav *juicefsiov1.WebDAV) error {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: webdav.Namespace,
		Name:      common.GenWebDAVName(webdav.Name),
	}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	desiredReplicas := int32(1)
	if webdav.Spec.Replicas != nil {
		desiredReplicas = *webdav.Spec.Replicas
	}

	phase := juicefsiov1.WebDAVPhasePending
	if deployment.Status.ReadyReplicas >= desiredReplicas {
		phase = juicefsiov1.WebDAVPhaseReady
	} else if deployment.Status.Replicas > 0 {
		phase = juicefsiov1.WebDAVPhaseProgressing
	}

	newStatus := juicefsiov1.WebDAVStatus{
		Phase:         phase,
		Replicas:      deployment.Status.Replicas,
		ReadyReplicas: deployment.Status.ReadyReplicas,
	}

	if reflect.DeepEqual(webdav.Status, newStatus) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &juicefsiov1.WebDAV{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: webdav.Namespace, Name: webdav.Name}, latest); err != nil {
			return err
		}
		latest.Status = newStatus
		return r.Status().Update(ctx, latest)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebDAVReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&juicefsiov1.WebDAV{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: common.MaxWebDAVConcurrentReconciles,
		}).
		Named("webdav").
		Complete(r)
}
