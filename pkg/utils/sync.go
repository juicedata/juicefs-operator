/*
Copyright 2025 Juicedata Inc

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

package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/crypto/ssh"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"

	corev1 "k8s.io/api/core/v1"
)

// GenerateSSHKeyPair ssh keys
func GenerateSSHKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", err
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	if privateKeyPEM == nil {
		return "", "", err
	}

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	return string(privateKeyPEM), string(publicKeyBytes), nil
}

func parseSinkValueToEnv(sink juicefsiov1.SyncSinkValue, syncName, key string) []corev1.EnvVar {
	if sink.Value != "" {
		return []corev1.EnvVar{
			{
				Name: key,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: common.GenSyncSecretName(syncName),
						},
						Key: key,
					},
				},
			},
		}
	}

	if sink.ValueFrom != nil {
		return []corev1.EnvVar{
			{
				Name:      key,
				ValueFrom: sink.ValueFrom,
			},
		}
	}
	return nil
}

func ParseSyncSink(sink juicefsiov1.SyncSink, syncName, ref string) (*juicefsiov1.ParsedSyncSink, error) {
	pss := &juicefsiov1.ParsedSyncSink{}
	var err error
	if sink.External != nil {
		ep, err := url.Parse(sink.External.Uri)
		if err != nil {
			return nil, err
		}
		ak := fmt.Sprintf("EXTERNAL_%s_%s", strings.ToUpper(ref), "ACCESS_KEY")
		sk := fmt.Sprintf("EXTERNAL_%s_%s", strings.ToUpper(ref), "SECRET_KEY")
		if sink.External.AccessKey.Value != "" || sink.External.AccessKey.ValueFrom != nil {
			pss.Envs = append(pss.Envs, parseSinkValueToEnv(sink.External.AccessKey, syncName, ak)...)
		}
		if sink.External.SecretKey.Value != "" || sink.External.SecretKey.ValueFrom != nil {
			pss.Envs = append(pss.Envs, parseSinkValueToEnv(sink.External.SecretKey, syncName, sk)...)
		}
		if len(pss.Envs) == 2 {
			ep.User = url.UserPassword(fmt.Sprintf("$%s", ak), fmt.Sprintf("$%s", sk))
		}
		pss.Uri = ep.String()
		return pss, nil
	}

	if sink.JuiceFS != nil {
		if ref == "TO" {
			if sink.JuiceFS.FilesFrom != nil {
				return nil, fmt.Errorf("cannot use filesFrom in `to` juicefs")
			}
		}
		if sink.JuiceFS.FilesFrom != nil && sink.JuiceFS.FilesFrom.Files != nil && sink.JuiceFS.FilesFrom.ConfigMap != nil {
			return nil, fmt.Errorf("cannot use files and configMap in same time")
		}
		pss.FilesFrom = sink.JuiceFS.FilesFrom
		if sink.JuiceFS.Path == "" {
			sink.JuiceFS.Path = "/"
		}
		pss.Uri, err = url.JoinPath("jfs://", sink.JuiceFS.VolumeName, sink.JuiceFS.Path)
		if err != nil {
			return nil, err
		}
		authCmd := []string{
			"juicefs",
			"auth",
			sink.JuiceFS.VolumeName,
		}
		suffix := "JUICEFS_" + strings.ToUpper(ref)
		if sink.JuiceFS.Token.Value != "" || sink.JuiceFS.Token.ValueFrom != nil {
			key := fmt.Sprintf("%s_%s", suffix, "TOKEN")
			pss.Envs = append(pss.Envs, parseSinkValueToEnv(sink.JuiceFS.Token, syncName, key)...)
			authCmd = append(authCmd, "--token", fmt.Sprintf("$%s", key))
		}
		if sink.JuiceFS.AccessKey.Value != "" || sink.JuiceFS.AccessKey.ValueFrom != nil {
			key := fmt.Sprintf("%s_%s", suffix, "ACCESS_KEY")
			pss.Envs = append(pss.Envs, parseSinkValueToEnv(sink.JuiceFS.AccessKey, syncName, key)...)
			authCmd = append(authCmd, "--access-key", fmt.Sprintf("$%s", key))
		}
		if sink.JuiceFS.SecretKey.Value != "" || sink.JuiceFS.SecretKey.ValueFrom != nil {
			key := fmt.Sprintf("%s_%s", suffix, "SECRET_KEY")
			pss.Envs = append(pss.Envs, parseSinkValueToEnv(sink.JuiceFS.SecretKey, syncName, key)...)
			authCmd = append(authCmd, "--secret-key", fmt.Sprintf("$%s", key))
		}
		if sink.JuiceFS.ConsoleUrl != "" {
			pss.Envs = append(pss.Envs, corev1.EnvVar{
				Name:  "BASE_URL",
				Value: sink.JuiceFS.ConsoleUrl,
			})
		}
		for _, opt := range sink.JuiceFS.AuthOptions {
			opt = strings.TrimPrefix(opt, "--")
			if opt == "token" || opt == "access-key" || opt == "secret-key" {
				continue
			}
			pair := strings.Split(opt, "=")
			if len(pair) == 2 {
				authCmd = append(authCmd, "--"+pair[0], pair[1])
				continue
			}
			authCmd = append(authCmd, "--"+pair[0])
		}
		if pss.FilesFrom != nil && pss.FilesFrom.Files != nil {
			filesCmd := fmt.Sprintf("mkdir %s\necho '%s' > %s/%s",
				common.SyncFileFromPath,
				strings.Join(pss.FilesFrom.Files, "\n"),
				common.SyncFileFromPath,
				common.SyncFileFromName)
			pss.PrepareCommand += filesCmd + "\n"
		}
		pss.PrepareCommand += strings.Join(authCmd, " ")
		return pss, nil
	}
	return nil, fmt.Errorf("invalid sync sink")
}

func IsDistributed(sync *juicefsiov1.Sync) bool {
	return sync.Spec.Replicas != nil && *sync.Spec.Replicas > 1
}
