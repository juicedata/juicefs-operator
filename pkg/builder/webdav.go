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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
)

const (
	webdavDefaultPort   int32 = 9007
	webdavContainerName       = "juicefs-webdav"
)

// WebDAVBuilder builds Kubernetes resources for a JuiceFS WebDAV server.
type WebDAVBuilder struct {
	webdav     *juicefsiov1.WebDAV
	secretData map[string]string
}

// NewWebDAVBuilder creates a new WebDAVBuilder.
func NewWebDAVBuilder(webdav *juicefsiov1.WebDAV, secret *corev1.Secret) *WebDAVBuilder {
	secretData := utils.ParseSecret(secret)
	return &WebDAVBuilder{
		webdav:     webdav,
		secretData: secretData,
	}
}

func (b *WebDAVBuilder) getPort() int32 {
	if b.webdav.Spec.Port > 0 {
		return b.webdav.Spec.Port
	}
	return webdavDefaultPort
}

func (b *WebDAVBuilder) isEEMode() bool {
	_, hasName := b.secretData["name"]
	_, hasToken := b.secretData["token"]
	return hasName && hasToken
}

func (b *WebDAVBuilder) genEnvs() []corev1.EnvVar {
	envs := []corev1.EnvVar{}

	// Mount secret as environment variables (only in EE mode where token exists)
	if b.webdav.Spec.SecretRef != nil && b.isEEMode() {
		for _, k := range secretStrippedEnvs {
			_, isOptional := secretStrippedEnvOptional[k]
			envs = append(envs, corev1.EnvVar{
				Name: secretStrippedEnvMap[k],
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:      k,
						Optional: &isOptional,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: b.webdav.Spec.SecretRef.Name,
						},
					},
				},
			})
		}
	}

	envs = append(envs, b.webdav.Spec.Env...)
	return envs
}

// genAuthCmd generates the auth command for JuiceFS EE, or empty string for CE.
func (b *WebDAVBuilder) genAuthCmd(ctx context.Context) string {
	if !b.isEEMode() {
		return ""
	}
	volName := b.secretData["name"]

	if b.secretData["initconfig"] != "" {
		// Use initconfig (pre-authenticated config file)
		return fmt.Sprintf("cp /etc/juicefs/%s.conf /root/.juicefs", volName)
	}

	authCmds := []string{
		common.JuiceFSBinary,
		"auth",
		volName,
	}

	for _, key := range secretKeys {
		if value, ok := b.secretData[key]; ok {
			if strippedKey, ok := secretStrippedEnvMap[key]; ok {
				authCmds = append(authCmds, "--"+key, "${"+strippedKey+"}")
			} else {
				authCmds = append(authCmds, "--"+key, value)
			}
		}
	}

	// add more options with key `format-options`
	if value, ok := b.secretData["format-options"]; ok {
		formatOptions := utils.ParseOptions(ctx, strings.Split(value, ","))
		for _, opt := range formatOptions {
			if opt[1] != "" {
				authCmds = append(authCmds, "--"+opt[0], opt[1])
			} else {
				authCmds = append(authCmds, "--"+opt[0])
			}
		}
	}

	return strings.Join(authCmds, " ")
}

// genWebDAVTarget returns the webdav target: either the volume name (EE) or metaurl (CE).
func (b *WebDAVBuilder) genWebDAVTarget() string {
	if volName, ok := b.secretData["name"]; ok && volName != "" {
		return volName
	}
	// CE mode: use metaurl
	if metaURL, ok := b.secretData["metaurl"]; ok && metaURL != "" {
		return metaURL
	}
	return ""
}

// genCommand generates the WebDAV server startup command.
func (b *WebDAVBuilder) genCommand(ctx context.Context) []string {
	authCmd := b.genAuthCmd(ctx)
	target := b.genWebDAVTarget()
	port := b.getPort()

	webdavCmds := []string{
		"exec",
		common.JuiceFSBinary,
		"webdav",
		target,
		fmt.Sprintf("0.0.0.0:%d", port),
	}

	for _, opt := range b.webdav.Spec.Options {
		opt = strings.TrimPrefix(opt, "--")
		pair := strings.SplitN(opt, "=", 2)
		if len(pair) == 2 {
			webdavCmds = append(webdavCmds, "--"+pair[0], pair[1])
		} else {
			webdavCmds = append(webdavCmds, "--"+pair[0])
		}
	}

	script := strings.Join(webdavCmds, " ")
	if authCmd != "" {
		script = authCmd + "\n" + script
	}

	return []string{"sh", "-c", script}
}

// genInitConfigVolumes generates the initconfig volume and mount if needed.
func (b *WebDAVBuilder) genInitConfigVolumes() ([]corev1.Volume, []corev1.VolumeMount) {
	if b.secretData["initconfig"] == "" || b.webdav.Spec.SecretRef == nil {
		return nil, nil
	}
	volName := b.secretData["name"]
	volumes := []corev1.Volume{
		{
			Name: common.InitConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: b.webdav.Spec.SecretRef.Name,
					Items: []corev1.KeyToPath{
						{
							Key:  common.InitConfigVolumeKey,
							Path: volName + ".conf",
						},
					},
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      common.InitConfigVolumeName,
			MountPath: common.InitConfigMountPath,
		},
	}
	return volumes, volumeMounts
}

// NewWebDAVDeployment creates a Deployment for the JuiceFS WebDAV server.
func (b *WebDAVBuilder) NewWebDAVDeployment(ctx context.Context) *appsv1.Deployment {
	name := common.GenWebDAVName(b.webdav.Name)
	replicas := int32(1)
	if b.webdav.Spec.Replicas != nil {
		replicas = *b.webdav.Spec.Replicas
	}

	labels := map[string]string{
		common.LabelWebDAV:    utils.TruncateLabelValue(b.webdav.Name),
		common.LabelAppType:   common.LabelWebDAVValue,
		common.LabelManagedBy: common.LabelManagedByValue,
	}

	podLabels := map[string]string{}
	for k, v := range labels {
		podLabels[k] = v
	}
	for k, v := range b.webdav.Spec.Labels {
		podLabels[k] = v
	}

	podAnnotations := map[string]string{}
	for k, v := range b.webdav.Spec.Annotations {
		podAnnotations[k] = v
	}

	imagePullSecrets := b.webdav.Spec.ImagePullSecrets
	if imagePullSecrets == nil {
		if common.OperatorPod != nil && common.OperatorPod.Namespace == b.webdav.Namespace {
			imagePullSecrets = common.OperatorPod.Spec.ImagePullSecrets
		}
	}

	initConfigVolumes, initConfigMounts := b.genInitConfigVolumes()
	command := b.genCommand(ctx)

	port := b.getPort()

	var resources corev1.ResourceRequirements
	if b.webdav.Spec.Resources != nil {
		resources = *b.webdav.Spec.Resources
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.webdav.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: common.GroupVersion,
					Kind:       common.KindWebDAV,
					Name:       b.webdav.Name,
					UID:        b.webdav.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.LabelWebDAV: utils.TruncateLabelValue(b.webdav.Name),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					NodeSelector:     b.webdav.Spec.NodeSelector,
					Tolerations:      b.webdav.Spec.Tolerations,
					Affinity:         b.webdav.Spec.Affinity,
					ImagePullSecrets: imagePullSecrets,
					Volumes:          initConfigVolumes,
					Containers: []corev1.Container{
						{
							Name:            webdavContainerName,
							Image:           b.webdav.Spec.Image,
							ImagePullPolicy: b.webdav.Spec.ImagePullPolicy,
							Command:         command,
							Env:             b.genEnvs(),
							Resources:       resources,
							VolumeMounts:    initConfigMounts,
							Ports: []corev1.ContainerPort{
								{
									Name:          "webdav",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: utils.ToPtr(true),
							},
						},
					},
				},
			},
		},
	}

	return deployment
}

// NewWebDAVService creates a Service to expose the JuiceFS WebDAV server.
func (b *WebDAVBuilder) NewWebDAVService() *corev1.Service {
	name := common.GenWebDAVName(b.webdav.Name)
	port := b.getPort()

	serviceType := b.webdav.Spec.ServiceType
	if serviceType == "" {
		serviceType = corev1.ServiceTypeClusterIP
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.webdav.Namespace,
			Labels: map[string]string{
				common.LabelWebDAV:    utils.TruncateLabelValue(b.webdav.Name),
				common.LabelAppType:   common.LabelWebDAVValue,
				common.LabelManagedBy: common.LabelManagedByValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: common.GroupVersion,
					Kind:       common.KindWebDAV,
					Name:       b.webdav.Name,
					UID:        b.webdav.UID,
					Controller: utils.ToPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				common.LabelWebDAV: utils.TruncateLabelValue(b.webdav.Name),
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "webdav",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	return svc
}
