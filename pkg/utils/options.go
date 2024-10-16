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
	"context"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// parseOptions parses the options string into a slice of key-value pairs.
func ParseOptions(ctx context.Context, options []string) [][]string {
	parsedOptions := make([][]string, 0, len(options))
	for _, option := range options {
		pair := strings.SplitN(strings.TrimSpace(option), "=", 2)
		if len(pair) == 2 && pair[1] == "" {
			log.FromContext(ctx).Info("invalid option", "option", option)
			continue
		}
		key := strings.TrimSpace(pair[0])
		if key == "" {
			continue
		}
		var value string
		if len(pair) == 1 {
			value = ""
		} else {
			value = strings.TrimSpace(pair[1])
		}
		parsedOptions = append(parsedOptions, []string{key, value})
	}
	return parsedOptions
}
