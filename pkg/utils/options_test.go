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
	"reflect"
	"testing"
)

func TestParseOptions(t *testing.T) {

	tests := []struct {
		name     string
		options  []string
		expected [][]string
	}{
		{
			name:     "Empty options",
			options:  []string{},
			expected: [][]string{},
		},
		{
			name:    "Single valid option",
			options: []string{"key=value"},
			expected: [][]string{
				{"key", "value"},
			},
		},
		{
			name:    "Multiple valid options",
			options: []string{"key1=value1", "key2=value2"},
			expected: [][]string{
				{"key1", "value1"},
				{"key2", "value2"},
			},
		},
		{
			name:     "ignore invalid options - no value",
			options:  []string{"key="},
			expected: [][]string{},
		},
		{
			name:     "ignore invalid options - no key",
			options:  []string{"=value"},
			expected: [][]string{},
		},
		{
			name:    "Option with spaces",
			options: []string{" key = value "},
			expected: [][]string{
				{"key", "value"},
			},
		},
		{
			name:    "Invalid option",
			options: []string{"key"},
			expected: [][]string{
				{"key", ""},
			},
		},
		{
			name:    "Mixed valid and invalid options",
			options: []string{"key1=value1", "invalid=", "key2=value2"},
			expected: [][]string{
				{"key1", "value1"},
				{"key2", "value2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseOptions(context.Background(), tt.options)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParseOptions() = %v, want %v", got, tt.expected)
			}
		})
	}
}
