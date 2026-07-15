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

package builder

import (
	"context"
	"strings"
	"testing"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeGateway(name, namespace string, secretRef *corev1.SecretEnvSource, metaURL, address string) *juicefsiov1.Gateway {
	return &juicefsiov1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "juicefs.io/v1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid",
		},
		Spec: juicefsiov1.GatewaySpec{
			Image:     "juicedata/mount:ee-5.1.1-test",
			SecretRef: secretRef,
			MetaURL:   metaURL,
			Address:   address,
		},
	}
}

func makeSecret(name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data:       data,
	}
}

func TestGatewayBuilder_EE_commands(t *testing.T) {
	secretRef := &corev1.SecretEnvSource{
		LocalObjectReference: corev1.LocalObjectReference{Name: "juicefs-secret"},
	}
	gw := makeGateway("gw-ee", "default", secretRef, "", "0.0.0.0:9000")
	secret := makeSecret("juicefs-secret", map[string][]byte{
		"name":       []byte("myvol"),
		"token":      []byte("mytoken"),
		"secret-key": []byte("mysecret"),
	})

	b := NewGatewayBuilder(gw, secret)
	cmds := b.genCommands(context.TODO())

	if len(cmds) != 3 || cmds[0] != "sh" || cmds[1] != "-c" {
		t.Fatalf("expected sh -c script, got %v", cmds)
	}
	script := cmds[2]

	if !strings.Contains(script, "juicefs auth myvol") {
		t.Errorf("expected auth command for EE, script: %s", script)
	}
	if !strings.Contains(script, "--token ${TOKEN}") {
		t.Errorf("expected --token flag, script: %s", script)
	}
	if !strings.Contains(script, "exec "+common.JuiceFSBinary+" gateway myvol 0.0.0.0:9000") {
		t.Errorf("expected gateway command, script: %s", script)
	}
}

func TestGatewayBuilder_CE_commands(t *testing.T) {
	gw := makeGateway("gw-ce", "default", nil, "redis://localhost/1", "0.0.0.0:9000")
	b := NewGatewayBuilder(gw, nil)
	cmds := b.genCommands(context.TODO())

	if len(cmds) != 3 || cmds[0] != "sh" || cmds[1] != "-c" {
		t.Fatalf("expected sh -c script, got %v", cmds)
	}
	script := cmds[2]

	if strings.Contains(script, "juicefs auth") {
		t.Errorf("CE should not have auth command, script: %s", script)
	}
	if !strings.Contains(script, "exec "+common.JuiceFSBinary+" gateway redis://localhost/1 0.0.0.0:9000") {
		t.Errorf("expected gateway command for CE, script: %s", script)
	}
}

func TestGatewayBuilder_gatewayPort(t *testing.T) {
	tests := []struct {
		address  string
		expected int32
	}{
		{"0.0.0.0:9000", 9000},
		{"0.0.0.0:8080", 8080},
		{"", int32(common.GatewayDefaultPort)},
	}
	for _, tt := range tests {
		gw := makeGateway("gw", "default", nil, "redis://localhost/1", tt.address)
		b := NewGatewayBuilder(gw, nil)
		if got := b.gatewayPort(); got != tt.expected {
			t.Errorf("gatewayPort(%q) = %d, want %d", tt.address, got, tt.expected)
		}
	}
}

func TestGatewayBuilder_NewDeployment(t *testing.T) {
	secretRef := &corev1.SecretEnvSource{
		LocalObjectReference: corev1.LocalObjectReference{Name: "juicefs-secret"},
	}
	gw := makeGateway("gw-test", "default", secretRef, "", "0.0.0.0:9000")
	secret := makeSecret("juicefs-secret", map[string][]byte{
		"name":  []byte("myvol"),
		"token": []byte("mytoken"),
	})
	b := NewGatewayBuilder(gw, secret)
	deploy := b.NewDeployment(context.TODO())

	if deploy.Name != GenGatewayName("gw-test") {
		t.Errorf("deployment name = %s, want %s", deploy.Name, GenGatewayName("gw-test"))
	}
	if deploy.Namespace != "default" {
		t.Errorf("deployment namespace = %s, want default", deploy.Namespace)
	}
	if len(deploy.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(deploy.Spec.Template.Spec.Containers))
	}
	c := deploy.Spec.Template.Spec.Containers[0]
	if c.Name != common.GatewayContainerName {
		t.Errorf("container name = %s, want %s", c.Name, common.GatewayContainerName)
	}
	if c.Image != gw.Spec.Image {
		t.Errorf("container image = %s, want %s", c.Image, gw.Spec.Image)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 9000 {
		t.Errorf("expected container port 9000, got %v", c.Ports)
	}
}

func TestGatewayBuilder_NewService(t *testing.T) {
	gw := makeGateway("gw-svc", "default", nil, "redis://localhost/1", "0.0.0.0:9000")
	gw.Spec.ServiceType = corev1.ServiceTypeLoadBalancer
	b := NewGatewayBuilder(gw, nil)
	svc := b.NewService()

	if svc.Name != GenGatewayName("gw-svc") {
		t.Errorf("service name = %s, want %s", svc.Name, GenGatewayName("gw-svc"))
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("service type = %s, want LoadBalancer", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 9000 {
		t.Errorf("expected service port 9000, got %v", svc.Spec.Ports)
	}
	if svc.Spec.Selector[common.LabelGateway] != "gw-svc" {
		t.Errorf("expected selector label, got %v", svc.Spec.Selector)
	}
}

func TestGenGatewayName(t *testing.T) {
	got := GenGatewayName("my-gw")
	want := common.GatewayNamePrefix + "-my-gw"
	if got != want {
		t.Errorf("GenGatewayName(%q) = %q, want %q", "my-gw", got, want)
	}
}
