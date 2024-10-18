/*
 * Copyright 2024 Juicedata Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package builder

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
)

func GenRoleBindingForWarmup(wu *juicefsiov1.WarmUp) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            common.GenRoleBindingName(wu.Name),
			Namespace:       wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(wu),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      common.GenSaName(wu.Name),
			Namespace: wu.Namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     common.GenRoleName(wu.Name),
		},
	}
}

func GenRoleForWarmup(wu *juicefsiov1.WarmUp) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            common.GenRoleName(wu.Name),
			Namespace:       wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(wu),
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"get"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			},
			{
				Verbs:     []string{"create"},
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
			},
		},
	}
}

func GenServiceAccount(wu *juicefsiov1.WarmUp) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            common.GenSaName(wu.Name),
			Namespace:       wu.Namespace,
			OwnerReferences: GetWarmUpOwnerReference(wu),
		},
	}
}
