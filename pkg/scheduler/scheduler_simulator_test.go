package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	TestNode1 = "node1"
	TestNode2 = "node2"
)

func makeNode(name string, labels map[string]string, taints []corev1.Taint) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: corev1.NodeSpec{
			Taints: taints,
		},
	}
}

func makePod(name string, affinity *corev1.Affinity, tolerations []corev1.Toleration, labels map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Affinity:    affinity,
			Tolerations: tolerations,
		},
	}
}

func TestCanSchedulePodOnNode_NilInputs(t *testing.T) {
	sim := NewSchedulerSimulator(fake.NewClientBuilder().Build(), []corev1.Node{})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), nil, nil)
	assert.False(t, ok)
	assert.NoError(t, err)
}

func TestCanSchedulePodOnNode_NodeAffinity(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"disktype": "ssd"}, nil)
	pod := makePod("pod1", &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "disktype",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"ssd"},
							},
						},
					},
				},
			},
		},
	}, nil, nil)
	sim := NewSchedulerSimulator(fake.NewClientBuilder().Build(), []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.True(t, ok)
	assert.NoError(t, err)

	// Node does not match affinity
	node2 := makeNode("node2", map[string]string{"disktype": "hdd"}, nil)
	ok, err = sim.CanSchedulePodOnNode(context.TODO(), &pod, &node2)
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestCanSchedulePodOnNode_Tolerations(t *testing.T) {
	taint := corev1.Taint{
		Key:    "dedicated",
		Value:  "gpu",
		Effect: corev1.TaintEffectNoSchedule,
	}
	node := makeNode(TestNode1, nil, []corev1.Taint{taint})

	// Pod does not tolerate taint
	pod := makePod("pod1", nil, nil, nil)
	sim := NewSchedulerSimulator(fake.NewClientBuilder().Build(), []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.False(t, ok)

	// Pod tolerates taint
	pod2 := makePod("pod2", nil, []corev1.Toleration{
		{
			Key:      "dedicated",
			Operator: corev1.TolerationOpEqual,
			Value:    "gpu",
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}, nil)
	ok, err = sim.CanSchedulePodOnNode(context.TODO(), &pod2, &node)
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestCanSchedulePodOnNode_PodAffinity(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"zone": "a"}, nil)
	otherPod := makePod("other", nil, nil, map[string]string{"app": "test"})
	otherPod.Spec.NodeName = TestNode1

	affinity := &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TopologyKey: "zone",
				},
			},
		},
	}
	pod := makePod("pod1", affinity, nil, nil)
	client := fake.NewClientBuilder().WithObjects(&otherPod).Build()
	sim := NewSchedulerSimulator(client, []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestCanSchedulePodOnNode_PodAffinity_NoMatch(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"zone": "a"}, nil)
	affinity := &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TopologyKey: "zone",
				},
			},
		},
	}
	pod := makePod("pod1", affinity, nil, nil)
	client := fake.NewClientBuilder().Build()
	sim := NewSchedulerSimulator(client, []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestCanSchedulePodOnNode_PodAntiAffinity(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"zone": "a"}, nil)
	otherPod := makePod("other", nil, nil, map[string]string{"app": "test"})
	otherPod.Spec.NodeName = TestNode1

	affinity := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TopologyKey: "zone",
				},
			},
		},
	}
	pod := makePod("pod1", affinity, nil, nil)
	client := fake.NewClientBuilder().WithObjects(&otherPod).Build()
	sim := NewSchedulerSimulator(client, []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestCanSchedulePodOnNode_PodAntiAffinity_NoConflict(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"zone": "a"}, nil)
	affinity := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TopologyKey: "zone",
				},
			},
		},
	}
	pod := makePod("pod1", affinity, nil, map[string]string{"app": "test"})
	client := fake.NewClientBuilder().Build()
	sim := NewSchedulerSimulator(client, []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.True(t, ok)

	pod.Spec.NodeName = TestNode1
	sim.AppendPendingPod(&pod)

	pod2 := makePod("pod2", affinity, nil, map[string]string{"app": "test"})
	ok, err = sim.CanSchedulePodOnNode(context.TODO(), &pod2, &node)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// Test that a pod with anti-affinity to its own label can be scheduled if no other pods exist
func TestCanSchedulePodOnNode_PodAntiAffinity_SelfReference(t *testing.T) {
	node := makeNode(TestNode1, map[string]string{"zone": "a"}, nil)
	affinity := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "self"},
					},
					TopologyKey: "zone",
				},
			},
		},
	}
	pod := makePod("pod1", affinity, nil, map[string]string{"app": "self"})
	client := fake.NewClientBuilder().Build()
	sim := NewSchedulerSimulator(client, []corev1.Node{node})
	ok, err := sim.CanSchedulePodOnNode(context.TODO(), &pod, &node)
	assert.NoError(t, err)
	assert.True(t, ok)
}
