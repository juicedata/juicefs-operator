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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
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

func FetchMetrics(ctx context.Context, sync *juicefsiov1.Sync) (map[string]float64, error) {
	// resp, err := http.Get(fmt.Sprintf("%s/metrics", url))
	// if err != nil {
	// 	return nil, err
	// }
	// defer resp.Body.Close()
	// if resp.StatusCode != http.StatusOK {
	// 	return nil, fmt.Errorf("failed to fetch metrics: %s", resp.Status)
	// }
	// data, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, err
	// }

	// parse metrics options
	port := 9567
	for _, opt := range sync.Spec.Options {
		if strings.Contains(opt, "metrics") {
			parts := strings.SplitN(opt, "=", 2)
			if len(parts) == 2 {
				metrics := strings.Split(parts[1], ":")
				if len(metrics) == 2 {
					port, _ = strconv.Atoi(metrics[1])
				}
			}
		}
	}
	stdout, stderr, err := ExecInPod(ctx,
		sync.Namespace,
		common.GenSyncManagerName(sync.Name),
		common.SyncNamePrefix,
		[]string{"curl", "-s", fmt.Sprintf("localhost:%d/metrics", port)},
	)
	if err != nil {
		return nil, err
	}
	if len(stderr) > 0 {
		return nil, fmt.Errorf("failed to fetch metrics: %s", stderr)
	}
	metrics, err := ParseSyncMetrics(string(stdout))
	if err != nil {
		return nil, err
	}
	return metrics, nil
}

// ParseSyncMetrics parse metrics from sync manager pod
// The format of the metrics is prometheus format
func ParseSyncMetrics(data string) (map[string]float64, error) {
	metrics := make(map[string]float64)
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		metricName := strings.SplitN(parts[0], "{", 2)[0]
		valStr := parts[1]
		value, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, err
		}
		metrics[metricName] = value
	}
	return metrics, nil
}

func parseBytes(s string) (int64, error) {
	re := regexp.MustCompile(`(?i)^(\d+\.?\d*)\s*([KMGTPE]i?B)?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid bytes format: %s", s)
	}

	num, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, err
	}

	unit := strings.ToLower(matches[2])
	switch unit {
	case "kib":
		num *= 1024
	case "mib":
		num *= math.Pow(1024, 2)
	case "gib":
		num *= math.Pow(1024, 3)
	case "tib":
		num *= math.Pow(1024, 4)
	case "pib":
		num *= math.Pow(1024, 5)
	case "eib":
		num *= math.Pow(1024, 6)
	case "b", "":
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
	return int64(num), nil

}

// ParseLog parse log from sync manager pod
func ParseLog(data string) (map[string]int64, error) {
	result := make(map[string]int64)
	data = strings.Split(data, "<INFO>:")[1]
	data = strings.Split(data, "[sync")[0]
	data = strings.TrimSpace(data)

	parts := strings.Split(data, ",")

	valRegex := regexp.MustCompile(`^(\d+)(?:\s+\((.*)\))?$`)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(kv[0]))
		valueStr := strings.TrimSpace(kv[1])

		matches := valRegex.FindStringSubmatch(valueStr)
		if matches == nil {
			continue
		}
		num, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		result[key] = int64(num)
		if len(matches) >= 3 && matches[2] != "" {
			bytes, err := parseBytes(matches[2])
			if err == nil {
				result[key+"_bytes"] = int64(bytes)
			}
		}
	}

	return result, nil
}
