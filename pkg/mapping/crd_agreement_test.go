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

	"sigs.k8s.io/yaml"
)

// pinnedVersions is the on-disk pin compared against supportedVersions and go.mod.
type pinnedVersions struct {
	Kubernetes struct {
		Version      string `json:"version"`
		GoModule     string `json:"goModule"`
		Distribution string `json:"distribution"`
	} `json:"kubernetes"`
	OPI struct {
		Group       string   `json:"group"`
		Version     string   `json:"version"`
		Commit      string   `json:"commit"`
		Fixtures    string   `json:"fixtures"`
		Kinds       []string `json:"kinds"`
		Repository  string   `json:"repository"`
		Upstream    string   `json:"upstream"`
		PullRequest string   `json:"pullRequest"`
		PatchState  string   `json:"patchState"`
		Ref         string   `json:"ref"`
	} `json:"opi"`
	DPF struct {
		Release       string   `json:"release"`
		Commit        string   `json:"commit"`
		Fixtures      string   `json:"fixtures"`
		MappedKinds   []string `json:"mappedKinds"`
		SupportWindow struct {
			MinInclusive string `json:"minInclusive"`
			MaxExclusive string `json:"maxExclusive"`
		} `json:"supportWindow"`
	} `json:"dpf"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "config", "versions.yaml")); err != nil {
		t.Fatalf("config/versions.yaml not found from %s: %v", root, err)
	}
	return root
}

func loadPinnedVersions(t *testing.T) pinnedVersions {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var v pinnedVersions
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatalf("config/versions.yaml: %v", err)
	}
	if v.Kubernetes.Version == "" || v.OPI.Version == "" || v.DPF.Release == "" {
		t.Fatal("config/versions.yaml is missing kubernetes, opi, or dpf version")
	}
	return v
}

func loadCRDIndex(t *testing.T, dirs ...string) map[string]map[string]any {
	t.Helper()
	idx := map[string]map[string]any{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var obj map[string]any
			if err := yaml.Unmarshal(b, &obj); err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			if obj["kind"] != "CustomResourceDefinition" {
				continue
			}
			spec, _ := obj["spec"].(map[string]any)
			names, _ := spec["names"].(map[string]any)
			kind, _ := names["kind"].(string)
			group, _ := spec["group"].(string)
			if kind == "" {
				continue
			}
			schema := storageSchema(spec)
			if schema == nil {
				t.Fatalf("%s: no storage OpenAPI schema", e.Name())
			}
			idx[group+"/"+kind] = schema
		}
	}
	return idx
}

func storageSchema(spec map[string]any) map[string]any {
	versions, _ := spec["versions"].([]any)
	for _, raw := range versions {
		v, _ := raw.(map[string]any)
		if v["storage"] != true {
			continue
		}
		schema, _ := v["schema"].(map[string]any)
		root, _ := schema["openAPIV3Schema"].(map[string]any)
		return root
	}
	return nil
}

func schemaHasPath(root map[string]any, dotted string) bool {
	return SchemaHasPath(root, dotted)
}

func resolveFromPath(from, forEachIn string) string {
	if strings.HasPrefix(from, "item.") {
		if forEachIn == "" {
			return ""
		}
		return forEachIn + "[]." + strings.TrimPrefix(from, "item.")
	}
	if strings.HasPrefix(from, "metadata.") {
		return "" // always present
	}
	return from
}

// TestMappingFieldsExistInVendoredCRDs is the API/schema agreement gate.
// A mapping `to:` that is not on the vendored CRD is an apply error or a
// silent prune waiting to happen. Compare CRDs to mappings before changing
// the pin in config/versions.yaml.
func TestMappingFieldsExistInVendoredCRDs(t *testing.T) {
	root := repoRoot(t)
	pin := loadPinnedVersions(t)
	specs, err := LoadDir(filepath.Join(root, "config", "mappings"))
	if err != nil {
		t.Fatal(err)
	}
	idx := loadCRDIndex(t,
		filepath.Join(root, pin.OPI.Fixtures),
		filepath.Join(root, pin.DPF.Fixtures),
		filepath.Join(root, "test", "conformance", "crds", "amd"),
	)

	mapped := map[string]bool{}
	for _, k := range pin.DPF.MappedKinds {
		mapped[k] = true
	}

	for _, spec := range specs {
		srcKey := spec.Source.Group + "/" + spec.Source.Kind
		srcSchema, ok := idx[srcKey]
		if !ok {
			t.Errorf("%s: no vendored CRD for source %s", spec.Metadata.Name, srcKey)
			continue
		}
		if spec.Source.Group != pin.OPI.Group || spec.Source.Version != pin.OPI.Version {
			t.Errorf("%s: source GVK %s/%s is not the pinned OPI %s/%s",
				spec.Metadata.Name, spec.Source.Group, spec.Source.Version, pin.OPI.Group, pin.OPI.Version)
		}
		for i, emit := range spec.Emit {
			tgtKey := emit.Target.Group + "/" + emit.Target.Kind
			tgtSchema, ok := idx[tgtKey]
			if !ok {
				t.Errorf("%s emit[%d]: no vendored CRD for target %s", spec.Metadata.Name, i, tgtKey)
				continue
			}
			if emit.Target.Group == "provisioning.dpu.nvidia.com" || emit.Target.Group == "svc.dpu.nvidia.com" {
				if !mapped[emit.Target.Kind] {
					t.Errorf("%s emit[%d]: %s is not in config/versions.yaml dpf.mappedKinds",
						spec.Metadata.Name, i, emit.Target.Kind)
				}
			}
			forEach := ""
			if emit.ForEach != nil {
				forEach = emit.ForEach.In
			}
			for _, field := range emit.Fields {
				if !schemaHasPath(tgtSchema, field.To) {
					t.Errorf("%s: %s %s is not on the vendored %s CRD",
						spec.Metadata.Name, emit.Target.Kind, field.To, tgtKey)
				}
				if from := resolveFromPath(field.From, forEach); from != "" && !schemaHasPath(srcSchema, from) {
					t.Errorf("%s: source path %s is not on the vendored %s CRD",
						spec.Metadata.Name, from, srcKey)
				}
			}
		}
	}
}

// TestPinnedVersionsAgree keeps the YAML pin, the runtime window, and go.mod
// on the same release. Pinning one place and leaving the others is how the
// v25 window survived a v26.4 lab install.
// TestSFCFixturesMatchPinnedHelmChartPatch stops the mapping from silently
// treating HelmChartSource as upstream-stable. opiproject/dpu-operator#6 is
// still open; main still requires NetworkFunction.image and has no chart.
// The fixtures and the mapping must track that unmerged pin.
func TestSFCFixturesMatchPinnedHelmChartPatch(t *testing.T) {
	pin := loadPinnedVersions(t)
	if pin.OPI.PullRequest == "" || pin.OPI.PatchState != "unmerged" {
		t.Fatal("SFC chart fields come from unmerged opiproject/dpu-operator#6; " +
			"pin pullRequest and patchState=unmerged in config/versions.yaml")
	}
	if !strings.Contains(pin.OPI.PullRequest, "dpu-operator/pull/6") {
		t.Errorf("opi.pullRequest = %q, want opiproject/dpu-operator#6", pin.OPI.PullRequest)
	}

	idx := loadCRDIndex(t, filepath.Join(repoRoot(t), pin.OPI.Fixtures))
	schema, ok := idx["config.openshift.io/ServiceFunctionChain"]
	if !ok {
		t.Fatal("vendored OPI fixtures have no ServiceFunctionChain CRD")
	}
	for _, p := range []string{
		"spec.networkFunctions[].chart",
		"spec.networkFunctions[].chart.name",
		"spec.networkFunctions[].chart.repository",
		"spec.networkFunctions[].chart.version",
		"spec.networkFunctions[].image",
		"spec.networkFunctions[].name",
		"status.conditions",
	} {
		if !schemaHasPath(schema, p) {
			t.Errorf("fixture SFC CRD is missing %s; it looks like upstream main, not PR #6", p)
		}
	}

	b, err := os.ReadFile(filepath.Join(repoRoot(t), pin.OPI.Fixtures, "config.openshift.io_servicefunctionchains.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "has(self.image) != has(self.chart)") {
		t.Error("fixture SFC CRD dropped the PR #6 CEL rule (exactly one of image or chart)")
	}

	spec, err := LoadFile(filepath.Join(repoRoot(t), "config", "mappings", "servicefunctionchain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Emit) == 0 {
		t.Fatal("servicefunctionchain mapping has no emits")
	}
	for i, emit := range spec.Emit {
		switch emit.Target.Kind {
		case "DPUService", "DPUServiceInterface":
			if emit.When != "'chart' in item" {
				t.Errorf("emit[%d] %s when=%q, want 'chart' in item; image-only NFs would be sent to DPF",
					i, emit.Target.Kind, emit.When)
			}
		case "DPUServiceChain":
			if !strings.Contains(emit.When, "chart") {
				t.Errorf("emit[%d] DPUServiceChain when=%q does not require a chart NF", i, emit.When)
			}
		default:
			t.Errorf("emit[%d] kind %s is not part of the direct ownership model", i, emit.Target.Kind)
		}
	}
}

func TestPinnedVersionsAgree(t *testing.T) {
	pin := loadPinnedVersions(t)

	// The NVIDIA window lives in pkg/lifecycle so the LCM can classify
	// without importing this test file. Re-read it through the public helper
	// after the window is updated in the same change as this pin.
	//
	// Import cycle: mapping must not import lifecycle. Compare the YAML to
	// itself for completeness here; version.go has its own well-formed test.
	// The cross-package check is TestPinnedNVIDIAWindow in lifecycle.
	if pin.DPF.SupportWindow.MinInclusive != pin.DPF.Release {
		t.Errorf("dpf.supportWindow.minInclusive %s != dpf.release %s; the window must start at the compared release",
			pin.DPF.SupportWindow.MinInclusive, pin.DPF.Release)
	}
	if pin.DPF.SupportWindow.MaxExclusive == "" {
		t.Error("dpf.supportWindow.maxExclusive is empty")
	}

	b, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := "k8s.io/apimachinery " + pin.Kubernetes.GoModule
	if !strings.Contains(string(b), want) {
		t.Errorf("go.mod does not pin %s (kubernetes.goModule in config/versions.yaml)", want)
	}
	if k8sMinor(pin.Kubernetes.Version) != k8sMinor(pin.Kubernetes.GoModule) {
		t.Errorf("kubernetes.version %s and goModule %s are different minors",
			pin.Kubernetes.Version, pin.Kubernetes.GoModule)
	}
}

func TestDpuOperatorConfigVendorIsConstrained(t *testing.T) {
	pin := loadPinnedVersions(t)
	b, err := os.ReadFile(filepath.Join(repoRoot(t), pin.OPI.Fixtures, "config.openshift.io_dpuoperatorconfigs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "vendor:") {
		t.Fatal("pinned DpuOperatorConfig CRD has no spec.vendor")
	}
	if !strings.Contains(body, "- nvidia") || !strings.Contains(body, "- amd") {
		t.Fatal("spec.vendor must be enum nvidia|amd; a free string is a Helm/OLM injection hole")
	}
	if strings.Contains(body, "x-kubernetes-preserve-unknown-fields: true") {
		t.Fatal("DpuOperatorConfig must not preserve unknown spec fields")
	}
}

func k8sMinor(ver string) string {
	ver = strings.TrimPrefix(ver, "v")
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return ver
	}
	// v1.36.4 and v0.36.0 both mean Kubernetes 1.36.
	if parts[0] == "0" || parts[0] == "1" {
		return parts[1]
	}
	return parts[1]
}
