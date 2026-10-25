/*
Copyright 2026 Kartikey Gupta.

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

package translation

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/mapping"
)

func condition(status string) map[string]any {
	return map[string]any{"type": "DPFReady", "status": status, "reason": "R", "message": "m"}
}

func firstCondition(t *testing.T, src *unstructured.Unstructured) map[string]any {
	t.Helper()
	conds, ok, _ := unstructured.NestedSlice(src.Object, "status", "conditions")
	if !ok || len(conds) == 0 {
		t.Fatalf("no status.conditions on source: %#v", src.Object["status"])
	}
	m, _ := conds[0].(map[string]any)
	return m
}

func TestApplyStatusSetsAndStampsTransitionTime(t *testing.T) {
	src := &unstructured.Unstructured{Object: map[string]any{}}
	desired := map[string]any{"emittedObjects": int64(2), "conditions": []any{condition("True")}}

	if !applyStatus(src, desired) {
		t.Fatal("expected first applyStatus to report a change")
	}
	if got, _, _ := unstructured.NestedInt64(src.Object, "status", "emittedObjects"); got != 2 {
		t.Errorf("emittedObjects=%d, want 2", got)
	}
	c := firstCondition(t, src)
	if c["status"] != "True" {
		t.Errorf("condition status=%v, want True", c["status"])
	}
	ltt, ok := c["lastTransitionTime"].(string)
	if !ok || ltt == "" {
		t.Fatalf("condition missing lastTransitionTime: %#v", c)
	}

	// Idempotent re-apply: no change, transition time preserved.
	if applyStatus(src, map[string]any{"emittedObjects": int64(2), "conditions": []any{condition("True")}}) {
		t.Error("expected no change on identical re-apply")
	}
	if got := firstCondition(t, src)["lastTransitionTime"]; got != ltt {
		t.Errorf("lastTransitionTime changed on no-op: %v != %v", got, ltt)
	}

	// Status flip: change reported, condition now False.
	if !applyStatus(src, map[string]any{"emittedObjects": int64(2), "conditions": []any{condition("False")}}) {
		t.Error("expected change when condition status flips")
	}
	if firstCondition(t, src)["status"] != "False" {
		t.Errorf("condition did not flip to False")
	}
}

func TestApplyStatusDoesNotWriteDaemonReady(t *testing.T) {
	src := &unstructured.Unstructured{Object: map[string]any{}}
	desired := map[string]any{"conditions": []any{
		map[string]any{"type": "Ready", "status": "True", "reason": "Stolen"},
		map[string]any{"type": "DPFReady", "status": "True", "reason": "R"},
	}}
	if !applyStatus(src, desired) {
		t.Fatal("expected DPFReady to be written")
	}
	conds, _, _ := unstructured.NestedSlice(src.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Ready" {
			t.Fatalf("merge wrote daemon-owned Ready: %#v", m)
		}
	}
	if firstCondition(t, src)["type"] != "DPFReady" {
		t.Errorf("kept condition=%v, want DPFReady", firstCondition(t, src)["type"])
	}
}

func TestUpdatingDPFStatusKeepsDaemonReady(t *testing.T) {
	spec, err := mapping.LoadFile("../../config/mappings/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ready := map[string]any{
		"type":               mapping.ReservedConditionType,
		"status":             "True",
		"reason":             "DeviceHealthy",
		"message":            "BlueField link up",
		"lastTransitionTime": "2026-10-25T05:37:47Z",
		"observedGeneration": int64(4),
	}
	src := map[string]any{
		"metadata": map[string]any{"name": "dh2-bluefield-2"},
		"spec": map[string]any{
			"dpuProductName": "BlueField-2",
			"nodeName":       "dh2",
		},
		"status": map[string]any{"conditions": []any{ready}},
	}
	child := map[string]any{
		"kind": "DPU",
		"status": map[string]any{
			"phase": "Ready",
			"conditions": []any{
				map[string]any{"type": "OSInstalled", "status": "True"},
			},
		},
	}
	desired, err := mapping.ApplyStatus(spec, src, []any{child, map[string]any{"kind": "DPUFlavor"}})
	if err != nil {
		t.Fatal(err)
	}
	obj := &unstructured.Unstructured{Object: src}
	if !applyStatus(obj, desired) {
		t.Fatal("expected DPFReady to be written")
	}
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	var gotReady, dpf map[string]any
	for _, raw := range conds {
		m, _ := raw.(map[string]any)
		switch m["type"] {
		case mapping.ReservedConditionType:
			gotReady = m
		case "DPFReady":
			dpf = m
		}
	}
	if gotReady["status"] != "True" || gotReady["reason"] != "DeviceHealthy" ||
		gotReady["message"] != "BlueField link up" ||
		gotReady["lastTransitionTime"] != "2026-10-25T05:37:47Z" ||
		gotReady["observedGeneration"] != int64(4) {
		t.Fatalf("daemon Ready was overwritten: %#v", gotReady)
	}
	for _, key := range []string{"type", "status", "reason", "message", "lastTransitionTime"} {
		if dpf[key] == nil || dpf[key] == "" {
			t.Fatalf("DPFReady missing schema field %s: %#v", key, dpf)
		}
	}
}
