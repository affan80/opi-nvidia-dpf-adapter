/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing limitations under the License.
*/

package translation

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestUndeclaredFieldPath(t *testing.T) {
	cases := []struct {
		err  string
		want string
	}{
		{
			err: "apply DPUService/hbn: failed to create typed patch object " +
				"(default/hbn; svc.dpu.nvidia.com/v1alpha1, Kind=DPUService): " +
				".spec.security: field not declared in schema",
			want: "spec.security",
		},
		{
			err: "DPUService in version \"v1alpha1\" cannot be handled as a DPUService: " +
				"strict decoding error: unknown field \"spec.security\"",
			want: "spec.security",
		},
		{
			err: "DpuOperatorConfig in version \"v1\" cannot be handled as a DpuOperatorConfig: " +
				"strict decoding error: unknown field \"spec.vendor\"",
			want: "spec.vendor",
		},
		{
			err: "DPU in version \"v1alpha1\" cannot be handled as a DPU: " +
				"strict decoding error: unknown field \"spec.astraEnabled\"",
			want: "spec.astraEnabled",
		},
		{
			err: "DPUDevice in version \"v1alpha1\" cannot be handled as a DPUDevice: " +
				"strict decoding error: unknown field \"spec.cluster\"",
			want: "spec.cluster",
		},
		{
			err:  `some other apply error`,
			want: "",
		},
	}
	for _, tc := range cases {
		got := undeclaredFieldPath(fmt.Errorf("%s", tc.err))
		if got != tc.want {
			t.Errorf("undeclaredFieldPath(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestDropFieldRemovesDeadSecurity(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"serviceID": "hbn",
			"security":  map[string]any{"privileged": true},
			"helmChart": map[string]any{"source": map[string]any{"chart": "hbn"}},
		},
	}}
	if !dropField(obj, "spec.security") {
		t.Fatal("dropField spec.security returned false")
	}
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	if _, ok := spec["security"]; ok {
		t.Fatalf("security still present: %#v", spec)
	}
	if spec["serviceID"] != "hbn" {
		t.Fatalf("dropped sibling fields: %#v", spec)
	}
}

func TestPruneUnknownSpecDropsSecurity(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"serviceID": "hbn",
			"security":  map[string]any{"privileged": true},
		},
	}}
	schema := map[string]any{
		"properties": map[string]any{
			"spec": map[string]any{
				"properties": map[string]any{
					"serviceID": map[string]any{"type": "string"},
				},
			},
		},
	}
	got := pruneUnknownSpec(obj, schema)
	if len(got) != 1 || got[0] != "spec.security" {
		t.Fatalf("dropped = %v", got)
	}
	if _, ok := obj.Object["spec"].(map[string]any)["security"]; ok {
		t.Fatal("security still present")
	}
	if obj.Object["spec"].(map[string]any)["serviceID"] != "hbn" {
		t.Fatal("dropped sibling")
	}
}

func TestOmitLiveUnknownFieldsRetries(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"security": map[string]any{"privileged": true}, "paused": false},
	}}
	err := fmt.Errorf(`.spec.security: field not declared in schema`)
	path, dropErr := omitLiveUnknownFields(obj, err)
	if dropErr != nil {
		t.Fatal(dropErr)
	}
	if path != "spec.security" {
		t.Fatalf("path = %q", path)
	}
	_, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "security")
	if found {
		t.Fatal("expected spec.security gone")
	}
}
