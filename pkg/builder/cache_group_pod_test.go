package builder

import (
	"context"
	"testing"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeAffinityWithoutReplicas(t *testing.T) {
	// Test that when replicas is nil, pods use nodeAffinity instead of nodeName
	cg := &juicefsiov1.CacheGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cg",
			Namespace: "default",
		},
		Spec: juicefsiov1.CacheGroupSpec{
			Replicas: nil, // This triggers the nodeAffinity path
			Worker: juicefsiov1.CacheGroupWorkerSpec{
				Template: juicefsiov1.CacheGroupWorkerTemplate{
					NodeSelector: map[string]string{
						"worker": "true",
					},
					Image: "juicedata/mount:latest",
				},
			},
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-secret",
				},
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"name":  []byte("test-volume"),
			"token": []byte("test-token"),
		},
	}

	nodeName := "worker-node-1"
	builder := NewPodBuilder(cg, secret, nodeName, cg.Spec.Worker.Template, false)
	pod := builder.NewCacheGroupWorker(context.Background())

	// Check that NodeName is not set
	if pod.Spec.NodeName != "" {
		t.Errorf("Expected NodeName to be empty, got %s", pod.Spec.NodeName)
	}

	// Check that NodeAffinity is set correctly
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		t.Fatal("Expected NodeAffinity to be set")
	}

	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected RequiredDuringSchedulingIgnoredDuringExecution to be set")
	}

	terms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) == 0 {
		t.Fatal("Expected at least one NodeSelectorTerm")
	}

	// Check that the term contains the hostname requirement
	found := false
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == "kubernetes.io/hostname" &&
				expr.Operator == corev1.NodeSelectorOpIn &&
				len(expr.Values) == 1 &&
				expr.Values[0] == nodeName {
				found = true
				break
			}
		}
	}

	if !found {
		t.Errorf("Expected NodeAffinity to contain hostname requirement for node %s", nodeName)
	}

	// Check that NodeSelector is still set
	if pod.Spec.NodeSelector == nil || pod.Spec.NodeSelector["worker"] != "true" {
		t.Error("Expected NodeSelector to be preserved")
	}
}

func TestNodeAffinityMergeWithUserAffinity(t *testing.T) {
	// Test that user-defined affinity is merged with node-specific affinity
	cg := &juicefsiov1.CacheGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cg",
			Namespace: "default",
		},
		Spec: juicefsiov1.CacheGroupSpec{
			Replicas: nil,
			Worker: juicefsiov1.CacheGroupWorkerSpec{
				Template: juicefsiov1.CacheGroupWorkerTemplate{
					NodeSelector: map[string]string{
						"worker": "true",
					},
					Image: "juicedata/mount:latest",
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Weight: 100,
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "zone",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"us-west-2a"},
											},
										},
									},
								},
							},
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "app",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"database"},
											},
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				},
			},
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-secret",
				},
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"name":  []byte("test-volume"),
			"token": []byte("test-token"),
		},
	}

	nodeName := "worker-node-1"
	builder := NewPodBuilder(cg, secret, nodeName, cg.Spec.Worker.Template, false)
	pod := builder.NewCacheGroupWorker(context.Background())

	// Check that both node-specific and user-defined affinities are present
	if pod.Spec.Affinity == nil {
		t.Fatal("Expected Affinity to be set")
	}

	// Check node-specific affinity (required)
	if pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected required NodeAffinity to be set")
	}

	// Check user-defined preferred node affinity
	if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil ||
		len(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
		t.Error("Expected user-defined preferred NodeAffinity to be preserved")
	}

	// Check user-defined pod anti-affinity
	if pod.Spec.Affinity.PodAntiAffinity == nil ||
		len(pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) == 0 {
		t.Error("Expected user-defined PodAntiAffinity to be preserved")
	}
}

func TestReplicasWithAntiAffinity(t *testing.T) {
	// Test that when replicas is set, pods still get anti-affinity
	replicas := int32(3)
	cg := &juicefsiov1.CacheGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cg",
			Namespace: "default",
		},
		Spec: juicefsiov1.CacheGroupSpec{
			Replicas: &replicas,
			Worker: juicefsiov1.CacheGroupWorkerSpec{
				Template: juicefsiov1.CacheGroupWorkerTemplate{
					NodeSelector: map[string]string{
						"worker": "true",
					},
					Image: "juicedata/mount:latest",
				},
			},
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-secret",
				},
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"name":  []byte("test-volume"),
			"token": []byte("test-token"),
		},
	}

	workerName := "worker-test-cg-0"
	builder := NewPodBuilder(cg, secret, workerName, cg.Spec.Worker.Template, false)
	pod := builder.NewCacheGroupWorker(context.Background())

	// Check that NodeName is not set (replicas mode)
	if pod.Spec.NodeName != "" {
		t.Errorf("Expected NodeName to be empty in replicas mode, got %s", pod.Spec.NodeName)
	}

	// Check that PodAntiAffinity is set
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		t.Fatal("Expected PodAntiAffinity to be set in replicas mode")
	}

	// Check that NodeSelector is still set
	if pod.Spec.NodeSelector == nil || pod.Spec.NodeSelector["worker"] != "true" {
		t.Error("Expected NodeSelector to be preserved in replicas mode")
	}
}