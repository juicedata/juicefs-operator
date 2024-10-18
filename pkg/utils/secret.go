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

package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

var (
	keysCompatible = map[string]string{
		"accesskey":  "access-key",
		"accesskey2": "access-key2",
		"secretkey":  "secret-key",
		"secretkey2": "secret-key2",
	}
)

func ParseSecret(secrets *corev1.Secret) map[string]string {
	secretData := make(map[string]string)
	for k, v := range secrets.Data {
		if realKey, ok := keysCompatible[k]; ok {
			secretData[realKey] = keysCompatible[k]
		} else {
			secretData[k] = string(v)
		}
	}
	return secretData
}

func ValidateSecret(secrets *corev1.Secret) error {
	if secrets == nil {
		return fmt.Errorf("secrets is nil")
	}
	secretData := ParseSecret(secrets)

	if _, ok := secretData["name"]; !ok {
		return fmt.Errorf("name is missing")
	}
	if _, ok := secretData["token"]; !ok {
		return fmt.Errorf("token is missing")
	}

	// TODO: validate other fields, like ak/sk, bucket, etc.
	return nil
}
