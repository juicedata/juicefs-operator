/*
Copyright 2025.

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
package scheduler

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1helper "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SchedulerSimulator struct {
	client          client.Client
	pendingPods     []*corev1.Pod
	toBeDeletedPods map[string]*corev1.Pod
	nodes           map[string]corev1.Node
}

func NewSchedulerSimulator(c client.Client, nodes []corev1.Node) *SchedulerSimulator {
	return &SchedulerSimulator{
		client:          c,
		pendingPods:     make([]*corev1.Pod, 0),
		toBeDeletedPods: make(map[string]*corev1.Pod),
		nodes:           lo.KeyBy(nodes, func(n corev1.Node) string { return n.Name }),
	}
}

// CanSchedulePodOnNode checks if a pod can be scheduled on a specific node
// This is a lightweight simulation and does not cover all scheduling scenarios
// It mainly checks node affinity, tolerations, pod affinity, and pod anti-affinity
// It does not consider resource requests, limits, or other constraints
// The returned result may be inaccurate, leading to scheduling failures and causing the Pod to remain in a Pending state.
func (s *SchedulerSimulator) CanSchedulePodOnNode(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	log := log.FromContext(ctx)
	if pod == nil || node == nil {
		return false, nil
	}
	log = log.WithValues("pod", pod.Name, "node", node.Name)
	if !s.checkNodeAffinity(pod, node) {
		log.V(1).Info("pod cannot be scheduled on node", "reason", "node affinity not match")
		return false, nil
	}

	if !s.checkTolerations(pod, node) {
		log.V(1).Info("pod cannot be scheduled on node", "reason", "tolerations not match")
		return false, nil
	}

	if v, err := s.checkPodAffinity(ctx, pod, node); err != nil || !v {
		log.V(1).Info("pod cannot be scheduled on node", "reason", "pod affinity not match")
		return false, err
	}

	if v, err := s.checkPodAntiAffinity(ctx, pod, node); err != nil || !v {
		log.V(1).Info("pod cannot be scheduled on node", "reason", "pod anti-affinity not match")
		return false, err
	}

	log.V(1).Info("pod can be scheduled on node")
	return true, nil
}

func (s *SchedulerSimulator) AppendPendingPod(pod *corev1.Pod) {
	s.pendingPods = append(s.pendingPods, pod)
}

func (s *SchedulerSimulator) AppendToBeDeletedPod(pod *corev1.Pod) {
	s.toBeDeletedPods[pod.Name] = pod
}

func (s *SchedulerSimulator) checkNodeAffinity(pod *corev1.Pod, node *corev1.Node) bool {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return true
	}
	fitsNodeAffinity, _ := nodeaffinity.GetRequiredNodeAffinity(pod).Match(node)
	return fitsNodeAffinity
}

func (s *SchedulerSimulator) checkTolerations(pod *corev1.Pod, node *corev1.Node) bool {
	_, hasUntoleratedTaint := v1helper.FindMatchingUntoleratedTaint(node.Spec.Taints, pod.Spec.Tolerations, func(t *corev1.Taint) bool {
		return t.Effect == corev1.TaintEffectNoExecute || t.Effect == corev1.TaintEffectNoSchedule
	})
	return !hasUntoleratedTaint
}

func (s *SchedulerSimulator) checkPodAffinity(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAffinity == nil {
		return true, nil
	}
	terms := pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) == 0 {
		return true, nil
	}
	for _, term := range terms {
		topologyValue, exists := node.Labels[term.TopologyKey]
		if !exists {
			return false, nil
		}
		pods, err := s.ListPodAffinityTermPods(ctx, pod.Name, term, topologyValue)
		if err != nil {
			return false, err
		}
		if len(pods) == 0 {
			return false, nil
		}
	}
	return true, nil
}

func (s *SchedulerSimulator) checkPodAntiAffinity(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		return true, nil
	}
	terms := pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) == 0 {
		return true, nil
	}
	for _, term := range terms {
		topologyValue, exists := node.Labels[term.TopologyKey]
		if !exists {
			// If the topology key doesn't exist, the anti-affinity constraint is considered satisfied.
			continue
		}
		pods, err := s.ListPodAffinityTermPods(ctx, pod.Name, term, topologyValue)
		if err != nil {
			return false, err
		}
		if len(pods) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// ListPodAffinityTermPods lists pods that match the given PodAffinityTerms
func (s *SchedulerSimulator) ListPodAffinityTermPods(ctx context.Context, currentPod string, term corev1.PodAffinityTerm, topologyValue string) ([]corev1.Pod, error) {
	var matchedPods []corev1.Pod
	selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector: %w", err)
	}
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: selector},
	}

	var podLists []corev1.PodList
	if len(term.Namespaces) > 0 {
		for _, ns := range term.Namespaces {
			var podList corev1.PodList
			nsListOpts := append([]client.ListOption{client.InNamespace(ns)}, listOpts...)
			if err := s.client.List(ctx, &podList, nsListOpts...); err != nil {
				return nil, fmt.Errorf("failed to list pods in namespace %s: %w", ns, err)
			}
			podLists = append(podLists, podList)
		}
	} else {
		var podList corev1.PodList
		if err := s.client.List(ctx, &podList, listOpts...); err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
		podLists = append(podLists, podList)
	}

	for _, podList := range podLists {
		for _, pod := range podList.Items {
			node, exists := s.nodes[pod.Spec.NodeName]
			if !exists {
				continue
			}
			if node.Labels[term.TopologyKey] == topologyValue && pod.Name != currentPod && s.toBeDeletedPods[pod.Name] == nil {
				matchedPods = append(matchedPods, pod)
			}
		}
	}
	// check pending pods as well
	for _, pod := range s.pendingPods {
		node, exists := s.nodes[pod.Spec.NodeName]
		if !exists {
			continue
		}
		if node.Labels[term.TopologyKey] == topologyValue && selector.Matches(labels.Set(pod.Labels)) && pod.Name != currentPod {
			matchedPods = append(matchedPods, *pod)
		}
	}
	return matchedPods, nil
}
