// Copyright 2026 Juicedata Inc
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

package v1

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	apivalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
)

func TestCacheDirValidation(t *testing.T) {
	crdYAML, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "juicefs.io_cachegroups.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	crdJSON, err := utilyaml.ToJSON(crdYAML)
	if err != nil {
		t.Fatal(err)
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := json.Unmarshal(crdJSON, crd); err != nil {
		t.Fatal(err)
	}

	var schemaV1 *apiextensionsv1.JSONSchemaProps
	for _, version := range crd.Spec.Versions {
		if version.Name == "v1" {
			schemaV1 = version.Schema.OpenAPIV3Schema
			break
		}
	}
	if schemaV1 == nil {
		t.Fatal("v1 schema not found")
	}

	schema := &apiextensions.JSONSchemaProps{}
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(schemaV1, schema, nil); err != nil {
		t.Fatal(err)
	}
	openAPIValidator, _, err := apivalidation.NewSchemaValidator(schema)
	if err != nil {
		t.Fatal(err)
	}
	structural, err := structuralschema.NewStructural(schema)
	if err != nil {
		t.Fatal(err)
	}
	celValidator := cel.NewValidator(structural, true, celconfig.PerCallLimit)
	if celValidator == nil {
		t.Fatal("CEL validator not found")
	}

	tests := []struct {
		name          string
		cacheDir      map[string]interface{}
		expectedError string
	}{
		{
			name: "HostPath",
			cacheDir: map[string]interface{}{
				"type": "HostPath",
				"path": "/var/jfs-cache",
			},
		},
		{
			name: "PVC block",
			cacheDir: map[string]interface{}{
				"type":       "PVC",
				"name":       "cache-pvc",
				"volumeMode": "Block",
				"format":     true,
			},
		},
		{
			name: "VolumeClaimTemplates block",
			cacheDir: map[string]interface{}{
				"type":   "VolumeClaimTemplates",
				"format": true,
				"volumeClaimTemplate": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "cache-template",
					},
					"spec": map[string]interface{}{
						"volumeMode": "Block",
					},
				},
			},
		},
		{
			name:          "type is required",
			cacheDir:      map[string]interface{}{},
			expectedError: "type: Required value",
		},
		{
			name: "HostPath requires path",
			cacheDir: map[string]interface{}{
				"type": "HostPath",
			},
			expectedError: "path is required when type is HostPath",
		},
		{
			name: "HostPath rejects empty path",
			cacheDir: map[string]interface{}{
				"type": "HostPath",
				"path": "",
			},
			expectedError: "path is required when type is HostPath",
		},
		{
			name: "PVC requires name",
			cacheDir: map[string]interface{}{
				"type": "PVC",
			},
			expectedError: "name is required when type is PVC",
		},
		{
			name: "PVC rejects empty name",
			cacheDir: map[string]interface{}{
				"type": "PVC",
				"name": "",
			},
			expectedError: "name is required when type is PVC",
		},
		{
			name: "VolumeClaimTemplates requires template",
			cacheDir: map[string]interface{}{
				"type": "VolumeClaimTemplates",
			},
			expectedError: "volumeClaimTemplate is required when type is VolumeClaimTemplates",
		},
		{
			name: "HostPath rejects volumeMode",
			cacheDir: map[string]interface{}{
				"type":       "HostPath",
				"path":       "/var/jfs-cache",
				"volumeMode": "Block",
			},
			expectedError: "volumeMode is only valid for PVC type",
		},
		{
			name: "VolumeClaimTemplates rejects top-level volumeMode",
			cacheDir: map[string]interface{}{
				"type":       "VolumeClaimTemplates",
				"volumeMode": "Block",
				"volumeClaimTemplate": map[string]interface{}{
					"spec": map[string]interface{}{
						"volumeMode": "Block",
					},
				},
			},
			expectedError: "volumeMode is only valid for PVC type",
		},
		{
			name: "HostPath rejects format",
			cacheDir: map[string]interface{}{
				"type":   "HostPath",
				"path":   "/var/jfs-cache",
				"format": true,
			},
			expectedError: "format is only valid for PVC and VolumeClaimTemplates types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := map[string]interface{}{
				"apiVersion": "juicefs.io/v1",
				"kind":       "CacheGroup",
				"metadata": map[string]interface{}{
					"name": "test",
				},
				"spec": map[string]interface{}{
					"worker": map[string]interface{}{
						"template": map[string]interface{}{
							"cacheDirs": []interface{}{tt.cacheDir},
						},
					},
				},
			}

			errs := apivalidation.ValidateCustomResource(nil, obj, openAPIValidator)
			celErrs, _ := celValidator.Validate(context.Background(), nil, structural, obj, nil, celconfig.RuntimeCELCostBudget)
			errs = append(errs, celErrs...)

			if tt.expectedError == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected validation errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected validation error containing %q", tt.expectedError)
			}
			if message := errs.ToAggregate().Error(); !strings.Contains(message, tt.expectedError) {
				t.Fatalf("validation error = %q, want it to contain %q", message, tt.expectedError)
			}
		})
	}
}
