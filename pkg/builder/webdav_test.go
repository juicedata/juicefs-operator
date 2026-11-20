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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
)

func newTestWebDAV(secretRefName string) *juicefsiov1.WebDAV {
	replicas := int32(1)
	return &juicefsiov1.WebDAV{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-webdav",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: juicefsiov1.WebDAVSpec{
			Image:    "juicedata/mount:ee-5.3.6-c8ec652",
			Replicas: &replicas,
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretRefName,
				},
			},
			Port: 9007,
		},
	}
}

func newTestSecret(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "juicefs-secret",
			Namespace: "default",
		},
		Data: data,
	}
}

func TestWebDAVBuilder_genAuthCmd_EE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"name":       []byte("my-volume"),
		"token":      []byte("my-token"),
		"access-key": []byte("my-ak"),
		"secret-key": []byte("my-sk"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	authCmd := b.genAuthCmd(context.Background())

	assert.Contains(t, authCmd, common.JuiceFSBinary+" auth my-volume")
	assert.Contains(t, authCmd, "--token ${TOKEN}")
	assert.Contains(t, authCmd, "--access-key my-ak")
	assert.Contains(t, authCmd, "--secret-key ${SECRET_KEY}")
}

func TestWebDAVBuilder_genAuthCmd_CE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"metaurl": []byte("redis://localhost:6379/0"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	authCmd := b.genAuthCmd(context.Background())

	// CE mode: no auth cmd
	assert.Equal(t, "", authCmd)
}

func TestWebDAVBuilder_genWebDAVTarget_EE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	target := b.genWebDAVTarget()
	assert.Equal(t, "my-volume", target)
}

func TestWebDAVBuilder_genWebDAVTarget_CE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"metaurl": []byte("redis://localhost:6379/0"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	target := b.genWebDAVTarget()
	assert.Equal(t, "redis://localhost:6379/0", target)
}

func TestWebDAVBuilder_genCommand_EE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	cmd := b.genCommand(context.Background())

	assert.Equal(t, "sh", cmd[0])
	assert.Equal(t, "-c", cmd[1])
	script := cmd[2]
	assert.Contains(t, script, common.JuiceFSBinary+" auth my-volume")
	assert.Contains(t, script, "exec "+common.JuiceFSBinary+" webdav my-volume 0.0.0.0:9007")
}

func TestWebDAVBuilder_genCommand_CE(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"metaurl": []byte("redis://localhost:6379/0"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	cmd := b.genCommand(context.Background())

	assert.Equal(t, "sh", cmd[0])
	assert.Equal(t, "-c", cmd[1])
	script := cmd[2]
	// CE mode: no auth cmd, just the webdav command
	assert.NotContains(t, script, "juicefs auth")
	assert.Contains(t, script, "exec "+common.JuiceFSBinary+" webdav redis://localhost:6379/0 0.0.0.0:9007")
}

func TestWebDAVBuilder_genCommand_WithOptions(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	webdav.Spec.Options = []string{"gzip", "threads=4"}
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	cmd := b.genCommand(context.Background())

	script := cmd[2]
	assert.Contains(t, script, "--gzip")
	assert.Contains(t, script, "--threads 4")
}

func TestWebDAVBuilder_NewWebDAVDeployment(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	deployment := b.NewWebDAVDeployment(context.Background())

	assert.Equal(t, common.GenWebDAVName(webdav.Name), deployment.Name)
	assert.Equal(t, webdav.Namespace, deployment.Namespace)
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
	assert.Equal(t, webdav.Spec.Image, deployment.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, int32(9007), deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
	assert.Equal(t, common.LabelManagedByValue, deployment.Labels[common.LabelManagedBy])
}

func TestWebDAVBuilder_NewWebDAVService(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	svc := b.NewWebDAVService()

	assert.Equal(t, common.GenWebDAVName(webdav.Name), svc.Name)
	assert.Equal(t, webdav.Namespace, svc.Namespace)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	assert.Equal(t, int32(9007), svc.Spec.Ports[0].Port)
}

func TestWebDAVBuilder_NewWebDAVService_NodePort(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	webdav.Spec.ServiceType = corev1.ServiceTypeNodePort
	secret := newTestSecret(map[string][]byte{
		"name":  []byte("my-volume"),
		"token": []byte("my-token"),
	})

	b := NewWebDAVBuilder(webdav, secret)
	svc := b.NewWebDAVService()

	assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
}

func TestWebDAVBuilder_getPort_Default(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	webdav.Spec.Port = 0 // not set
	secret := newTestSecret(map[string][]byte{})

	b := NewWebDAVBuilder(webdav, secret)
	assert.Equal(t, int32(webdavDefaultPort), b.getPort())
}

func TestWebDAVBuilder_getPort_Custom(t *testing.T) {
	webdav := newTestWebDAV("juicefs-secret")
	webdav.Spec.Port = 8080
	secret := newTestSecret(map[string][]byte{})

	b := NewWebDAVBuilder(webdav, secret)
	assert.Equal(t, int32(8080), b.getPort())
}
