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
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"

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

func parseExternalSyncSink(external *juicefsiov1.SyncSinkExternal, syncName, ref string) (*juicefsiov1.ParsedSyncSink, error) {
	pss := &juicefsiov1.ParsedSyncSink{}
	pss.FilesFrom = external.FilesFrom
	ep, err := url.Parse(external.Uri)
	if err != nil {
		return nil, err
	}
	ak := fmt.Sprintf("EXTERNAL_%s_%s", strings.ToUpper(ref), "ACCESS_KEY")
	sk := fmt.Sprintf("EXTERNAL_%s_%s", strings.ToUpper(ref), "SECRET_KEY")
	if external.AccessKey.Value != "" || external.AccessKey.ValueFrom != nil {
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(external.AccessKey, syncName, ak)...)
	}
	if external.SecretKey.Value != "" || external.SecretKey.ValueFrom != nil {
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(external.SecretKey, syncName, sk)...)
	}
	if len(pss.Envs) == 2 {
		ep.User = url.UserPassword(fmt.Sprintf("$%s", ak), fmt.Sprintf("$%s", sk))
	}
	pss.Uri = ep.String()
	pss.ExtraVolumes = external.ExtraVolumes
	return pss, nil
}

func parseJuiceFSSyncSink(jfs *juicefsiov1.SyncSinkJuiceFS, syncName, ref string) (*juicefsiov1.ParsedSyncSink, error) {
	pss := &juicefsiov1.ParsedSyncSink{}
	var err error
	pss.FilesFrom = jfs.FilesFrom
	pss.Uri, err = url.JoinPath("jfs://", jfs.VolumeName, jfs.Path)
	if err != nil {
		return nil, err
	}
	authCmd := []string{
		"juicefs",
		"auth",
		jfs.VolumeName,
	}
	suffix := "JUICEFS_" + strings.ToUpper(ref)
	if jfs.Token.Value != "" || jfs.Token.ValueFrom != nil {
		key := fmt.Sprintf("%s_%s", suffix, "TOKEN")
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(jfs.Token, syncName, key)...)
		authCmd = append(authCmd, "--token", fmt.Sprintf("$%s", key))
	}
	if jfs.AccessKey.Value != "" || jfs.AccessKey.ValueFrom != nil {
		key := fmt.Sprintf("%s_%s", suffix, "ACCESS_KEY")
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(jfs.AccessKey, syncName, key)...)
		authCmd = append(authCmd, "--access-key", fmt.Sprintf("$%s", key))
	}
	if jfs.SecretKey.Value != "" || jfs.SecretKey.ValueFrom != nil {
		key := fmt.Sprintf("%s_%s", suffix, "SECRET_KEY")
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(jfs.SecretKey, syncName, key)...)
		authCmd = append(authCmd, "--secret-key", fmt.Sprintf("$%s", key))
	}
	if jfs.ConsoleUrl != "" {
		pss.Envs = append(pss.Envs, corev1.EnvVar{
			Name:  "BASE_URL",
			Value: jfs.ConsoleUrl,
		})
	}
	for _, opt := range jfs.AuthOptions {
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
	pss.PrepareCommand += strings.Join(authCmd, " ")
	pss.ExtraVolumes = jfs.ExtraVolumes
	return pss, nil
}

func parseJuiceFSCeSyncSink(jfs *juicefsiov1.SyncSinkJuiceFSCE, syncName, ref string) (*juicefsiov1.ParsedSyncSink, error) {
	pss := &juicefsiov1.ParsedSyncSink{}
	var err error
	metaUrl, err := url.Parse(jfs.MetaURL)
	volName := "JUICEFS_CE_" + strings.ToUpper(ref)
	if err != nil {
		return nil, err
	}
	if jfs.MetaPassWord.Value != "" || jfs.MetaPassWord.ValueFrom != nil {
		username := ""
		if metaUrl.User != nil {
			username = metaUrl.User.Username()
		}
		password := fmt.Sprintf("JUICEFS_CE_%s_%s", strings.ToUpper(ref), "META_PASSWORD")
		metaUrl.User = url.UserPassword(username, fmt.Sprintf("$%s", password))
		pss.Envs = append(pss.Envs, parseSinkValueToEnv(jfs.MetaPassWord, syncName, password)...)
	}
	pss.PrepareCommand += fmt.Sprintf("export %s=%s", volName, metaUrl.String())
	pss.FilesFrom = jfs.FilesFrom
	pss.Uri, err = url.JoinPath("jfs://", volName, jfs.Path)

	if err != nil {
		return nil, err
	}
	pss.ExtraVolumes = jfs.ExtraVolumes
	return pss, nil
}

func ParseSyncSink(sink juicefsiov1.SyncSink, syncName, ref string) (*juicefsiov1.ParsedSyncSink, error) {
	var pss *juicefsiov1.ParsedSyncSink
	var err error
	if sink.External != nil {
		pss, err = parseExternalSyncSink(sink.External, syncName, ref)
	}
	if sink.JuiceFS != nil {
		pss, err = parseJuiceFSSyncSink(sink.JuiceFS, syncName, ref)
	}
	if sink.JuiceFSCE != nil {
		pss, err = parseJuiceFSCeSyncSink(sink.JuiceFSCE, syncName, ref)
	}
	if err != nil {
		return nil, err
	}
	if pss == nil {
		return nil, fmt.Errorf("invalid sync sink")
	}

	if ref == "TO" && pss.FilesFrom != nil {
		return nil, fmt.Errorf("filesFrom is not supported in sync sink TO")
	}

	if pss.FilesFrom != nil && pss.FilesFrom.Files != nil {
		filesCmd := fmt.Sprintf("mkdir %s\necho '%s' > %s/%s",
			common.SyncFileFromPath,
			strings.Join(pss.FilesFrom.Files, "\n"),
			common.SyncFileFromPath,
			common.SyncFileFromName)
		pss.PrepareCommand += "\n" + filesCmd
	}
	if pss.FilesFrom != nil && pss.FilesFrom.FilePath != "" {
		if strings.HasSuffix(pss.FilesFrom.FilePath, "/") {
			return nil, fmt.Errorf("invalid filesfrom filePath: %s", pss.FilesFrom.FilePath)
		}
		filePath, err := url.JoinPath(pss.Uri, pss.FilesFrom.FilePath)
		if err != nil {
			return nil, err
		}
		filesCmd := []string{
			"mkdir -p " + common.SyncFileFromPath,
			"&&",
			"juicefs",
			"sync",
			filePath,
			path.Join(common.SyncFileFromPath, common.SyncFileFromName),
		}
		pss.PrepareCommand += "\n" + strings.Join(filesCmd, " ")
	}
	return pss, nil
}

func IsDistributed(sync *juicefsiov1.Sync) bool {
	return sync.Spec.Replicas != nil && *sync.Spec.Replicas > 1
}

func GetMetricsPortWithOptions(opts []string) int32 {
	port := 9567
	for _, opt := range opts {
		if strings.Contains(opt, "metrics") {
			parts := strings.SplitN(opt, "=", 2)
			if len(parts) == 2 {
				metrics := strings.Split(parts[1], ":")
				if len(metrics) == 2 {
					port, _ = strconv.Atoi(metrics[1])
					if port == 0 {
						port = 9567
					}
					break
				}
			}
		}
	}
	return int32(port)
}

func FetchMetrics(ctx context.Context, sync *juicefsiov1.Sync) (map[string]float64, error) {
	// parse metrics options
	port := GetMetricsPortWithOptions(sync.Spec.Options)
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
	metrics, err := ParseSyncMetrics(stdout)
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
	logWithStats := ""
	for _, line := range strings.Split(data, "\n") {
		if strings.Contains(line, "<INFO>: Found") {
			logWithStats = line
			break
		}
	}
	if logWithStats == "" {
		return nil, fmt.Errorf("failed to log, cannot found stats log")
	}
	result := make(map[string]int64)
	logWithStats = strings.Split(logWithStats, "<INFO>:")[1]
	logWithStats = strings.Split(logWithStats, "[sync")[0]
	logWithStats = strings.TrimSpace(logWithStats)

	parts := strings.Split(logWithStats, ",")

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
				result[key+"_bytes"] = bytes
			}
		}
	}

	return result, nil
}

func CalculateProgress(a, b int64) string {
	if b == 0 {
		return "0.00%"
	}
	progress := math.Trunc((float64(a)/float64(b))*10000) / 100
	return fmt.Sprintf("%.2f%%", progress)
}

func GenCronSyncJobName(cronName string, now time.Time) string {
	return fmt.Sprintf("%s-%s", cronName, now.Format("20060102150405"))
}

func TruncateSyncAffinityIfNeeded(affinity *corev1.Affinity) {
	if affinity == nil {
		return
	}

	handleTerm := func(term *corev1.PodAffinityTerm) {
		if term == nil {
			return
		}
		if term.LabelSelector == nil {
			return
		}
		if term.LabelSelector.MatchLabels != nil {
			for k, v := range term.LabelSelector.MatchLabels {
				if strings.Contains(k, common.LabelSync) {
					term.LabelSelector.MatchLabels[k] = TruncateLabelValue(v)
				}
			}
		}
		if term.LabelSelector.MatchExpressions != nil {
			for j, expr := range term.LabelSelector.MatchExpressions {
				if expr.Key == common.LabelSync {
					term.LabelSelector.MatchExpressions[j].Values = []string{TruncateLabelValue(expr.Values[0])}
				}
			}
		}
	}

	if affinity.PodAntiAffinity != nil {
		for _, term := range affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
			handleTerm(&term.PodAffinityTerm)
		}
		for _, term := range affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			handleTerm(&term)
		}
	}

	if affinity.PodAffinity != nil {
		for _, term := range affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
			handleTerm(&term.PodAffinityTerm)
		}
		for _, term := range affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			handleTerm(&term)
		}
	}
}
