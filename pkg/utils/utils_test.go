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
	"testing"
)

func TestGenHash(t *testing.T) {
	tests := []struct {
		name   string
		object interface{}
		want   string
	}{
		{
			name:   "Test with string",
			object: "test string",
			want:   "ee68a16fef8bf44a2b86b5614554b4079820f98dea14a67c3b507f59333cd591",
		},
		{
			name:   "Test with int",
			object: 12345,
			want:   "5994471abb01112afcc18159f6cc74b4f511b99806da59b3caf5a9c173cacfc5",
		},
		{
			name:   "Test with struct",
			object: struct{ Name string }{Name: "test"},
			want:   "3a7e9639e5a126efa16e6c730f9cbb141b0e7eac714e3c9388c7b308a146888c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenHash(tt.object); got != tt.want {
				t.Errorf("GenHash() = %v, want = %v", got, tt.want)
			}
		})
	}
}
func TestContainsNodeSelector(t *testing.T) {
	tests := []struct {
		name   string
		expect map[string]string
		target map[string]string
		want   bool
	}{
		{
			name:   "Test with matching selectors",
			expect: map[string]string{"key1": "value1", "key2": "value2"},
			target: map[string]string{"key1": "value1", "key2": "value2", "key3": "value3"},
			want:   true,
		},
		{
			name:   "Test with non-matching selectors",
			expect: map[string]string{"key1": "value1", "key2": "value2"},
			target: map[string]string{"key1": "value1", "key2": "different_value"},
			want:   false,
		},
		{
			name:   "Test with empty expect map",
			expect: map[string]string{},
			target: map[string]string{"key1": "value1"},
			want:   true,
		},
		{
			name:   "Test with empty target map",
			expect: map[string]string{"key1": "value1"},
			target: map[string]string{},
			want:   false,
		},
		{
			name:   "Test with both maps empty",
			expect: map[string]string{},
			target: map[string]string{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NodeSelectorContains(tt.expect, tt.target); got != tt.want {
				t.Errorf("ContainsNodeSelector() = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestCompareImageVersion(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		want    int
	}{
		{
			name:    "Test with current greater than target",
			current: "juicedata/mount:ee-1.2.3",
			target:  "1.2.2",
			want:    1,
		},
		{
			name:    "Test with current less than target",
			current: "juicedata/mount:ee-1.2.2",
			target:  "1.2.3",
			want:    -1,
		},
		{
			name:    "Test with current equal to target",
			current: "juicedata/mount:ee-1.2.3",
			target:  "1.2.3",
			want:    0,
		},
		{
			name:    "Test with current having less parts than target",
			current: "juicedata/mount:ee-1.2",
			target:  "1.2.3",
			want:    -1,
		},
		{
			name:    "Test with specific version",
			current: "juicedata/mount:ee-nightly",
			target:  "1.2.1",
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareEEImageVersion(tt.current, tt.target); got != tt.want {
				t.Errorf("CompareImageVersion() = %v, want = %v", got, tt.want)
			}
		})
	}
}
