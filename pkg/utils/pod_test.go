// Copyright 2025 Juicedata Inc
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
	"reflect"
	"testing"
)

func TestParseWorkerMountCmds(t *testing.T) {
	tests := []struct {
		name            string
		cmds            string
		expectedVolName string
		expectedOptions []string
	}{
		{
			name:            "valid command with options",
			cmds:            "exec /sbin/mount.juicefs test-vol /mnt/jfs -o option1,option2",
			expectedVolName: "test-vol",
			expectedOptions: []string{"option1", "option2"},
		},
		{
			name:            "valid command without options",
			cmds:            "exec /sbin/mount.juicefs test-vol /mnt/jfs -o ",
			expectedVolName: "test-vol",
			expectedOptions: []string{""},
		},
		{
			name: "valid worker command with block cache setup",
			cmds: `/usr/bin/juicefs auth test-vol || exit 1
CACHE_DEVICE=/dev/jfs-cache-dir-0
exec /sbin/mount.juicefs test-vol /mnt/jfs -o option1,option2`,
			expectedVolName: "test-vol",
			expectedOptions: []string{"option1", "option2"},
		},
		{
			name: "valid worker command any command",
			cmds: `/usr/bin/juicefs auth test-vol || exit 1
CACHE_DEVICE=/dev/jfs-cache-dir-0
asd
asd
asd
asd as || exit 1
asd
exec /sbin/mount.juicefs test-vol /mnt/jfs -o option1,option2`,
			expectedVolName: "test-vol",
			expectedOptions: []string{"option1", "option2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volName, options := MustParseWorkerMountCmds(tt.cmds)
			if volName != tt.expectedVolName {
				t.Errorf("ParseMountCmds() volName = %v, want %v", volName, tt.expectedVolName)
			}
			if !reflect.DeepEqual(options, tt.expectedOptions) {
				t.Errorf("ParseMountCmds() options = %v, want %v", options, tt.expectedOptions)
			}
		})
	}
}
