// Copyright 2025 Juicedata Inc
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

package builder

import (
	"testing"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTestSync returns a minimal Sync object for use in builder tests.
func newTestSync(name string, replicas int32, env []corev1.EnvVar) *juicefsiov1.Sync {
	return &juicefsiov1.Sync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: juicefsiov1.SyncSpec{
			Image:    "juicedata/mount:ce-v1.3.0",
			Replicas: utils.ToPtr(replicas),
			Env:      env,
		},
	}
}

// newTestSyncPodBuilder creates a SyncPodBuilder with simple from/to sinks.
func newTestSyncPodBuilder(sc *juicefsiov1.Sync) *SyncPodBuilder {
	from := &juicefsiov1.ParsedSyncSink{Uri: "s3://src/"}
	to := &juicefsiov1.ParsedSyncSink{Uri: "s3://dst/"}
	return NewSyncPodBuilder(sc, from, to)
}

func TestSyncPodBuilder_genManagerEnvs_UserEnvsFirst(t *testing.T) {
	userEnvs := []corev1.EnvVar{
		{Name: "MY_VAR", Value: "user-value"},
		{Name: "ANOTHER_VAR", Value: "another"},
	}
	sc := newTestSync("test-sync", 1, userEnvs)
	b := newTestSyncPodBuilder(sc)

	envs := b.genManagerEnvs()

	if len(envs) < 2 {
		t.Fatalf("expected at least 2 envs, got %d", len(envs))
	}
	if envs[0].Name != "MY_VAR" || envs[0].Value != "user-value" {
		t.Errorf("expected first env to be MY_VAR=user-value, got %v", envs[0])
	}
	if envs[1].Name != "ANOTHER_VAR" || envs[1].Value != "another" {
		t.Errorf("expected second env to be ANOTHER_VAR=another, got %v", envs[1])
	}
}

func TestSyncPodBuilder_genManagerEnvs_OperatorEnvsOverrideUserEnvs(t *testing.T) {
	// User tries to set WORKER_IPS; the operator-managed value must win (appear last).
	userEnvs := []corev1.EnvVar{
		{Name: "WORKER_IPS", Value: "10.0.0.1"},
	}
	sc := newTestSync("test-sync", 3, userEnvs)
	b := newTestSyncPodBuilder(sc)
	b.UpdateWorkerIPs([]string{"192.168.1.1", "192.168.1.2"})

	envs := b.genManagerEnvs()

	// Find the last occurrence of WORKER_IPS – it must be the operator value.
	var lastWorkerIPs string
	for _, e := range envs {
		if e.Name == "WORKER_IPS" {
			lastWorkerIPs = e.Value
		}
	}
	expected := "192.168.1.1,192.168.1.2"
	if lastWorkerIPs != expected {
		t.Errorf("expected last WORKER_IPS=%q, got %q", expected, lastWorkerIPs)
	}
}

func TestSyncPodBuilder_genManagerEnvs_NoUserEnvs(t *testing.T) {
	sc := newTestSync("test-sync", 1, nil)
	b := newTestSyncPodBuilder(sc)

	envs := b.genManagerEnvs()

	// No user envs defined – verify no unexpected user-defined vars are present.
	for _, e := range envs {
		if e.Name == "MY_VAR" || e.Name == "ANOTHER_VAR" || e.Name == "CUSTOM_ENV" {
			t.Errorf("unexpected user-defined env var %q found when spec.Env is nil", e.Name)
		}
	}
}

func TestSyncPodBuilder_WorkerPod_UserEnvsInjected(t *testing.T) {
	userEnvs := []corev1.EnvVar{
		{Name: "MY_VAR", Value: "worker-value"},
	}
	sc := newTestSync("test-sync", 3, userEnvs)
	b := newTestSyncPodBuilder(sc)

	pods := b.NewWorkerPods()
	if len(pods) == 0 {
		t.Fatal("expected at least one worker pod")
	}
	container := pods[0].Spec.Containers[0]
	found := false
	for _, e := range container.Env {
		if e.Name == "MY_VAR" && e.Value == "worker-value" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected MY_VAR=worker-value in worker pod env, got %v", container.Env)
	}
}

func TestSyncPodBuilder_ManagerPod_UserEnvsInjected(t *testing.T) {
	userEnvs := []corev1.EnvVar{
		{Name: "CUSTOM_ENV", Value: "custom-value"},
	}
	sc := newTestSync("test-sync", 1, userEnvs)
	b := newTestSyncPodBuilder(sc)

	pod := b.NewManagerPod()
	container := pod.Spec.Containers[0]
	found := false
	for _, e := range container.Env {
		if e.Name == "CUSTOM_ENV" && e.Value == "custom-value" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CUSTOM_ENV=custom-value in manager pod env, got %v", container.Env)
	}
}

func TestSyncPodBuilder_WorkerPod_NoEnvWhenSpecEmpty(t *testing.T) {
	sc := newTestSync("test-sync", 3, nil)
	b := newTestSyncPodBuilder(sc)

	pods := b.NewWorkerPods()
	if len(pods) == 0 {
		t.Fatal("expected at least one worker pod")
	}
	container := pods[0].Spec.Containers[0]
	// No user envs defined – verify no unexpected user-defined vars are present.
	for _, e := range container.Env {
		if e.Name == "MY_VAR" || e.Name == "ANOTHER_VAR" || e.Name == "CUSTOM_ENV" {
			t.Errorf("unexpected user-defined env var %q found when spec.Env is nil", e.Name)
		}
	}
}
