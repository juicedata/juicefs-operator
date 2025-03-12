// Copyright 2024 Juicedata Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"reflect"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *CacheGroupReconciler) ListAllCgs(ctx context.Context) ([]juicefsiov1.CacheGroup, error) {
	cgs := juicefsiov1.CacheGroupList{}
	if err := r.List(ctx, &cgs); err != nil {
		return nil, err
	}
	return cgs.Items, nil
}

func (r *CacheGroupReconciler) enqueueRequestForNode() handler.EventHandler {
	return &handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.TypedCreateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			// FIXME: do not enqueue when first run
			log.FromContext(ctx).V(1).Info("enqueueRequestForNode: watching node created, enqueue all cache groups")
			cgs, err := r.ListAllCgs(ctx)
			if err == nil {
				for _, cg := range cgs {
					w.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cg.Name, Namespace: cg.Namespace}})
				}
			}
		},
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			old, new := e.ObjectOld, e.ObjectNew
			if !reflect.DeepEqual(old.GetLabels(), new.GetLabels()) {
				log.FromContext(ctx).V(1).Info("enqueueRequestForNode: watching node labels change, enqueue all cache groups")
				cgs, err := r.ListAllCgs(ctx)
				if err == nil {
					for _, cg := range cgs {
						w.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cg.Name, Namespace: cg.Namespace}})
					}
				}
			}
		},
		DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			cgs, err := r.ListAllCgs(ctx)
			if err == nil {
				for _, cg := range cgs {
					log.FromContext(ctx).V(1).Info("enqueueRequestForNode: watching node deleted, enqueue all cache groups")
					w.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cg.Name, Namespace: cg.Namespace}})
				}
			}
		},
	}
}
