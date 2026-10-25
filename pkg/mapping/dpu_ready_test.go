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
	"fmt"
	"strings"
	"testing"
)

func blueFieldSource(annotations map[string]any) map[string]any {
	meta := map[string]any{
		"name": "dh2-bluefield-2",
		"annotations": map[string]any{
			"provisioning.dpu.nvidia.com/serial-number": "MT2321XZ0KVX",
			"dpu.nvidia.com/bfb-url":                    "https://example.invalid/bf.bfb",
		},
	}
	for k, v := range annotations {
		meta["annotations"].(map[string]any)[k] = v
	}
	return object(meta, map[string]any{
		"dpuProductName": "BlueField-2",
		"nodeName":       "dh2",
	})
}

func appliedFlavorNvconfig(t *testing.T, src map[string]any) string {
	t.Helper()
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	objs, err := Apply(spec, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() == "DPUFlavor" {
			return fmt.Sprint(nested(o.Object, "spec", "nvconfig"))
		}
	}
	t.Fatal("no DPUFlavor emitted")
	return ""
}

func TestApplyFlavorNvconfigMatchesCurrentSFAnnotations(t *testing.T) {
	// dh2 hostagent queued next_boot=0 because the flavor was empty.
	// Matching current firmware (PF_TOTAL_SF=32, PF_SF_BAR_SIZE=8) is the
	// DPF-API way to drop reset_needed without mlxfwreset.
	got := appliedFlavorNvconfig(t, blueFieldSource(map[string]any{
		"dpu.nvidia.com/pf-total-sf":    "32",
		"dpu.nvidia.com/pf-sf-bar-size": "8",
	}))
	if !strings.Contains(got, "PF_TOTAL_SF=32") || !strings.Contains(got, "PF_SF_BAR_SIZE=8") {
		t.Fatalf("flavor nvconfig must honor current-SF annotations, got %s", got)
	}
	if strings.Contains(got, "PF_TOTAL_SF=0") || strings.Contains(got, "PF_SF_BAR_SIZE=0") {
		t.Fatalf("zero SF nvconfig is the reboot stall, got %s", got)
	}
}

func TestApplyFlavorNvconfigRejectsZeroSFAnnotation(t *testing.T) {
	got := appliedFlavorNvconfig(t, blueFieldSource(map[string]any{
		"dpu.nvidia.com/pf-total-sf":    "0",
		"dpu.nvidia.com/pf-sf-bar-size": "0",
	}))
	if !strings.Contains(got, "PF_TOTAL_SF=20") || !strings.Contains(got, "PF_SF_BAR_SIZE=10") {
		t.Fatalf("zero annotation must fall back to official 26.4.0 HBN, got %s", got)
	}
}

func TestDefaultFlavorPinsOfficialDPF264Nvconfig(t *testing.T) {
	got := appliedFlavorNvconfig(t, blueFieldSource(nil))
	for _, want := range []string{"PF_TOTAL_SF=20", "PF_SF_BAR_SIZE=10", "PF_BAR2_ENABLE=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("default nvconfig missing official 26.4.0 %s: %s", want, got)
		}
	}
}

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

func TestDPFReadyFalseWhileOSInstalling(t *testing.T) {
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := object(map[string]any{"name": "dh2-bluefield-2"}, map[string]any{
		"dpuProductName": "BlueField-2",
		"nodeName":       "dh2",
	})
	dpu := map[string]any{
		"kind": "DPU",
		"status": map[string]any{
			"phase": "OS Installing",
			"conditions": []any{
				map[string]any{"type": "OSInstalled", "status": "False"},
				map[string]any{"type": "FWConfigured", "status": "True"},
			},
		},
	}
	st, err := ApplyStatus(spec, src, []any{dpu, map[string]any{"kind": "DPUFlavor"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := dpfReadyStatus(t, st); got != "False" {
		t.Fatalf("DPFReady=%s, want False while DPU phase is OS Installing", got)
	}
}

func TestDPFReadyTrueWhenDPUPhaseReadyWithoutReadyCondition(t *testing.T) {
	// Live NVIDIA DPU objects publish BFBPrepared/OSInstalled/… and status.phase,
	// not type=Ready. Requiring Ready made DPFReady stay False after provision.
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := object(map[string]any{"name": "dh2-bluefield-2"}, map[string]any{
		"dpuProductName": "BlueField-2",
		"nodeName":       "dh2",
	})
	dpu := map[string]any{
		"kind": "DPU",
		"status": map[string]any{
			"phase": "Ready",
			"conditions": []any{
				map[string]any{"type": "OSInstalled", "status": "True"},
				map[string]any{"type": "FWConfigured", "status": "True"},
			},
		},
	}
	bfb := map[string]any{"kind": "BFB", "status": map[string]any{"phase": "Ready"}}
	st, err := ApplyStatus(spec, src, []any{dpu, bfb, map[string]any{"kind": "DPUFlavor"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := dpfReadyStatus(t, st); got != "True" {
		t.Fatalf("DPFReady=%s, want True when DPU phase is Ready", got)
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
