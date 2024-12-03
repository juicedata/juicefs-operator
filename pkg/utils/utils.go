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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func ToPtr[T any](v T) *T {
	return &v
}

func SliceContains[T comparable](arr []T, v T) bool {
	for _, a := range arr {
		if a == v {
			return true
		}
	}
	return false
}

func NodeSelectorContains(expect, target map[string]string) bool {
	for k, v := range expect {
		if target[k] != v {
			return false
		}
	}
	return true
}

// GenHash generates a hash string for the object, using sha256
func GenHash(object interface{}) string {
	data, _ := json.Marshal(object)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func MustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// CompareImageVersion compares two image versions
// return 1 if image > target
// return -1 if image < target
// return 0 if image == target
func CompareEEImageVersion(image, target string) int {
	current := strings.Split(image, ":")[1]
	current = strings.ReplaceAll(current, "ee-", "")
	if strings.Contains(current, "latest") ||
		strings.Contains(current, "nightly") ||
		strings.Contains(current, "dev") {
		return 1
	}

	currentParts := strings.Split(current, ".")
	targetParts := strings.Split(target, ".")
	for i := 0; i < len(currentParts); i++ {
		if i >= len(targetParts) {
			return 1
		}
		v1, _ := strconv.Atoi(currentParts[i])
		v2, _ := strconv.Atoi(targetParts[i])
		if v1 > v2 {
			return 1
		} else if v1 < v2 {
			return -1
		}
	}

	if len(currentParts) < len(targetParts) {
		return -1
	}

	return 0
}
