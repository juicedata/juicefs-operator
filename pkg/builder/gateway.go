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
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

// GatewayBuilder builds the Kubernetes resources for a JuiceFS Gateway.
type GatewayBuilder struct {
	gw         *juicefsiov1.Gateway
	secretData map[string]string
}

// NewGatewayBuilder creates a new GatewayBuilder.
func NewGatewayBuilder(gw *juicefsiov1.Gateway, secret *corev1.Secret) *GatewayBuilder {
	b := &GatewayBuilder{gw: gw}
	if secret != nil {
		b.secretData = utils.ParseSecret(secret)
	}
	return b
}

// isEE returns true when the gateway uses JuiceFS Enterprise Edition (secretRef).
func (b *GatewayBuilder) isEE() bool {
	return b.gw.Spec.SecretRef != nil
}

// volName returns the JuiceFS volume name extracted from the secret (EE only).
func (b *GatewayBuilder) volName() string {
	if b.secretData != nil {
		return b.secretData["name"]
	}
	return ""
}

// address returns the listening address for the gateway.
func (b *GatewayBuilder) address() string {
	if b.gw.Spec.Address != "" {
		return b.gw.Spec.Address
	}
	return common.GatewayDefaultAddress
}

// gatewayPort parses the port from the address string.
func (b *GatewayBuilder) gatewayPort() int32 {
	addr := b.address()
	parts := strings.Split(addr, ":")
	if len(parts) == 2 {
		if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 && p <= 65535 {
			return int32(p)
		}
	}
	return int32(common.GatewayDefaultPort)
}

// GenGatewayName returns the name for all gateway resources.
func GenGatewayName(gwName string) string {
	return fmt.Sprintf("%s-%s", common.GatewayNamePrefix, gwName)
}

// commonLabels returns labels shared by all gateway resources.
func (b *GatewayBuilder) commonLabels() map[string]string {
	return map[string]string{
		common.LabelManagedBy: common.LabelManagedByValue,
		common.LabelGateway:   utils.TruncateLabelValue(b.gw.Name),
		common.LabelAppType:   common.LabelGatewayValue,
	}
}

// genEnvs builds environment variables for the gateway container.
func (b *GatewayBuilder) genEnvs() []corev1.EnvVar {
	envs := make([]corev1.EnvVar, 0)

	if b.isEE() && b.gw.Spec.SecretRef != nil {
		// Inject TOKEN from the secret so auth command can use it.
		optional := false
		envs = append(envs,
			corev1.EnvVar{
				Name: "TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: b.gw.Spec.SecretRef.Name},
						Key:                  "token",
						Optional:             &optional,
					},
				},
			},
		)
		// SECRET_KEY / SECRET_KEY_2 may be optional.
		secretKeyOptional := true
		for _, entry := range []struct{ envKey, secretKey string }{
			{"SECRET_KEY", "secret-key"},
			{"SECRET_KEY_2", "secret-key2"},
		} {
			envs = append(envs, corev1.EnvVar{
				Name: entry.envKey,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: b.gw.Spec.SecretRef.Name},
						Key:                  entry.secretKey,
						Optional:             &secretKeyOptional,
					},
				},
			})
		}
	}

	envs = append(envs, b.gw.Spec.Env...)
	return envs
}

// genAuthCmd builds the `juicefs auth` command for EE.
func (b *GatewayBuilder) genAuthCmd(ctx context.Context) string {
	volName := b.volName()
	authArgs := []string{common.JuiceFSBinary, "auth", volName, "--token", "${TOKEN}"}

	if b.secretData != nil {
		for _, key := range []string{"access-key", "access-key2", "bucket", "bucket2", "subdir"} {
			if v, ok := b.secretData[key]; ok && v != "" {
				authArgs = append(authArgs, "--"+key, v)
			}
		}
		for _, key := range []string{"secret-key", "secret-key2"} {
			if _, ok := b.secretData[key]; ok {
				envKey := "SECRET_KEY"
				if key == "secret-key2" {
					envKey = "SECRET_KEY_2"
				}
				authArgs = append(authArgs, "--"+key, "${"+envKey+"}")
			}
		}
		if value, ok := b.secretData["format-options"]; ok {
			opts := utils.ParseOptions(ctx, strings.Split(value, ","))
			for _, opt := range opts {
				if opt[1] != "" {
					authArgs = append(authArgs, "--"+opt[0], opt[1])
				} else {
					authArgs = append(authArgs, "--"+opt[0])
				}
			}
		}
	}
	return strings.Join(authArgs, " ")
}

// genGatewayCmd builds the `juicefs gateway` command.
func (b *GatewayBuilder) genGatewayCmd() string {
	var target string
	if b.isEE() {
		target = b.volName()
	} else {
		target = b.gw.Spec.MetaURL
	}

	args := []string{"exec", common.JuiceFSBinary, "gateway", target, b.address()}
	args = append(args, b.gw.Spec.Options...)
	return strings.Join(args, " ")
}

// genCommands returns the shell command to run in the gateway container.
func (b *GatewayBuilder) genCommands(ctx context.Context) []string {
	var script string
	if b.isEE() {
		script = b.genAuthCmd(ctx) + "\n" + b.genGatewayCmd()
	} else {
		script = b.genGatewayCmd()
	}
	return []string{"sh", "-c", script}
}

// NewDeployment constructs the gateway Deployment.
func (b *GatewayBuilder) NewDeployment(ctx context.Context) *appsv1.Deployment {
	name := GenGatewayName(b.gw.Name)
	labels := b.commonLabels()

	podLabels := make(map[string]string, len(labels)+len(b.gw.Spec.Labels))
	for k, v := range labels {
		podLabels[k] = v
	}
	for k, v := range b.gw.Spec.Labels {
		podLabels[k] = v
	}

	podAnnotations := make(map[string]string, len(b.gw.Spec.Annotations))
	for k, v := range b.gw.Spec.Annotations {
		podAnnotations[k] = v
	}

	replicas := b.gw.Spec.Replicas

	container := corev1.Container{
		Name:            common.GatewayContainerName,
		Image:           b.gw.Spec.Image,
		ImagePullPolicy: b.gw.Spec.ImagePullPolicy,
		Command:         b.genCommands(ctx),
		Env:             b.genEnvs(),
		Ports: []corev1.ContainerPort{
			{
				Name:          "gateway",
				ContainerPort: b.gatewayPort(),
				Protocol:      corev1.ProtocolTCP,
			},
		},
	}
	if b.gw.Spec.Resources != nil {
		container.Resources = *b.gw.Spec.Resources
	}

	imagePullSecrets := b.gw.Spec.ImagePullSecrets
	if imagePullSecrets == nil {
		if common.OperatorPod != nil && common.OperatorPod.Namespace == b.gw.Namespace {
			imagePullSecrets = common.OperatorPod.Spec.ImagePullSecrets
		}
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.gw.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: b.gw.APIVersion,
					Kind:       b.gw.Kind,
					Name:       b.gw.Name,
					UID:        b.gw.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.LabelGateway: utils.TruncateLabelValue(b.gw.Name),
					common.LabelAppType: common.LabelGatewayValue,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers:       []corev1.Container{container},
					ImagePullSecrets: imagePullSecrets,
					NodeSelector:     b.gw.Spec.NodeSelector,
					Tolerations:      b.gw.Spec.Tolerations,
					Affinity:         b.gw.Spec.Affinity,
				},
			},
		},
	}

	hash := utils.GenHash(deploy)
	deploy.Annotations = map[string]string{
		common.LabelWorkerHash: hash,
	}

	return deploy
}

// NewService constructs the gateway Service.
func (b *GatewayBuilder) NewService() *corev1.Service {
	name := GenGatewayName(b.gw.Name)
	labels := b.commonLabels()

	serviceAnnotations := make(map[string]string, len(b.gw.Spec.ServiceAnnotations))
	for k, v := range b.gw.Spec.ServiceAnnotations {
		serviceAnnotations[k] = v
	}

	serviceType := b.gw.Spec.ServiceType
	if serviceType == "" {
		serviceType = corev1.ServiceTypeClusterIP
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   b.gw.Namespace,
			Labels:      labels,
			Annotations: serviceAnnotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: b.gw.APIVersion,
					Kind:       b.gw.Kind,
					Name:       b.gw.Name,
					UID:        b.gw.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				common.LabelGateway: utils.TruncateLabelValue(b.gw.Name),
				common.LabelAppType: common.LabelGatewayValue,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "gateway",
					Port:       b.gatewayPort(),
					TargetPort: intstr.FromInt32(b.gatewayPort()),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
