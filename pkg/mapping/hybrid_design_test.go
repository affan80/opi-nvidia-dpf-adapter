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

package mapping

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hybridDesignPath = "../../docs/hybrid-integration-design.md"

// requiredDesignSections is TASK 01.06: the spec is the implementation
// contract only if these topics are written down, not implied by code.
var requiredDesignSections = []string{
	"## Version constraints",
	"## Install strategy",
	"## Config annotations",
	"## Service-chain resource graph",
	"## Status ownership",
	"## Uninstall policy",
}

func TestHybridDesignDocIsTheContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "hybrid-integration-design.md"))
	if err != nil {
		t.Fatalf("docs/hybrid-integration-design.md missing: %v (TASK 01.06: write the spec before irreversible implementation)", err)
	}
	doc := string(b)
	if !strings.Contains(doc, "single implementation contract") {
		t.Error("design doc does not declare itself the single implementation contract")
	}
	for _, h := range requiredDesignSections {
		if !strings.Contains(doc, h) {
			t.Errorf("design doc missing %q", h)
		}
	}

	pin := loadPinnedVersions(t)
	for _, want := range []string{
		pin.Kubernetes.Version,
		pin.DPF.Release,
		pin.DPF.SupportWindow.MinInclusive,
		pin.DPF.SupportWindow.MaxExclusive,
		pin.OPI.Commit,
		"patchState: unmerged",
		"lifecycle.opi.nvidia.com/vendor",
		"UnknownVendor",
		"SanitizeVendorToken",
		"DPUService",
		"DPUServiceInterface",
		"DPUServiceChain",
		"DPUDeployment",
		"DPFReady",
		"DSCReady",
		"vsp-opi-daemon",
		"translation.opi.nvidia.com/cleanup",
		"make undeploy",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("design doc does not record %q; it has drifted from the pinned contract", want)
		}
	}

	tab, err := LoadConditionWriterTable(filepath.Join(repoRoot(t), "config", "condition-writers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range tab.Writers {
		if !strings.Contains(doc, w.Type) {
			t.Errorf("design doc omits condition type %s from the writer table", w.Type)
		}
		if !strings.Contains(doc, w.Writer) {
			t.Errorf("design doc omits writer %s", w.Writer)
		}
	}

	spec, err := LoadFile(filepath.Join(repoRoot(t), "config", "mappings", "servicefunctionchain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, emit := range spec.Emit {
		if !strings.Contains(doc, emit.Target.Kind) {
			t.Errorf("design doc omits generated kind %s from the resource graph", emit.Target.Kind)
		}
	}
}

func TestHybridDesignPathConstantMatchesRepo(t *testing.T) {
	if _, err := os.Stat(hybridDesignPath); err != nil {
		t.Fatal(err)
	}
}
