/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    10|Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mapping

import (
	"testing"
)

func TestDPFReadyFalseWhileDPUInitializing(t *testing.T) {
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := object(map[string]any{"name": "dh2-bluefield-2"}, map[string]any{
		"dpuProductName": "BlueField-2",
		"nodeName":       "dh2",
	})
	children := []any{
		map[string]any{"kind": "DPUDevice"},
		map[string]any{"kind": "DPUFlavor"},
		map[string]any{"status": map[string]any{"phase": "Initializing"}},
		map[string]any{"status": map[string]any{"phase": "Initializing"}},
	}
	st, err := ApplyStatus(spec, src, children)
	if err != nil {
		t.Fatal(err)
	}
	if got := dpfReadyStatus(t, st); got != "False" {
		t.Fatalf("DPFReady=%s, want False while DPU/BFB phase is Initializing", got)
	}
}

func TestDPFReadyTrueWhenReadyConditionSet(t *testing.T) {
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := object(map[string]any{"name": "dh2-bluefield-2"}, map[string]any{
		"dpuProductName": "BlueField-2",
		"nodeName":       "dh2",
	})
	ready := map[string]any{
		"status": map[string]any{
			"phase": "Initializing",
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
			},
		},
	}
	st, err := ApplyStatus(spec, src, []any{ready, map[string]any{"kind": "DPUFlavor"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := dpfReadyStatus(t, st); got != "True" {
		t.Fatalf("DPFReady=%s, want True once a child reports Ready", got)
	}
}

func dpfReadyStatus(t *testing.T, st map[string]any) string {
	t.Helper()
	conds, ok := st["conditions"].([]any)
	if !ok || len(conds) == 0 {
		t.Fatalf("conditions=%v", st["conditions"])
	}
	c0, ok := conds[0].(map[string]any)
	if !ok {
		t.Fatalf("condition=%T", conds[0])
	}
	s, _ := c0["status"].(string)
	return s
}
