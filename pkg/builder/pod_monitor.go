/*
 * Copyright 2025 Juicedata Inc
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
	"maps"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewSyncPodMonitor(sync *juicefsiov1.Sync) *monitoringv1.PodMonitor {
	labels := map[string]string{
		common.LabelSync:    sync.Name,
		common.LabelAppType: common.LabelSyncManagerValue,
	}
	maps.Copy(labels, sync.Spec.PodMonitor.Labels)

	podMonitorName := common.GenSyncPodMonitorName(sync.Name)
	podMonitorNamespace := sync.Namespace
	if sync.Spec.PodMonitor.Namespace != "" {
		podMonitorNamespace = sync.Spec.PodMonitor.Namespace
	}

	podMonitor := &monitoringv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podMonitorName,
			Namespace: podMonitorNamespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: common.GroupVersion,
					Kind:       common.KindSync,
					Name:       sync.Name,
					UID:        sync.UID,
					Controller: lo.ToPtr(true),
				},
			},
		},
		Spec: monitoringv1.PodMonitorSpec{
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Port:          utils.ToPtr("metrics"),
					Path:          "/metrics",
					Interval:      "5s",
					ScrapeTimeout: "5s",
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.LabelSync:    sync.Name,
					common.LabelAppType: common.LabelSyncManagerValue,
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{sync.Namespace},
			},
		},
	}

	return podMonitor
}
