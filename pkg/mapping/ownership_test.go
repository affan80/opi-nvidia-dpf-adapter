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
	"path/filepath"
	"slices"
	"testing"
)

// Direct SFC children. DPUDeployment is forbidden because its controller
// also creates these three kinds.
var directSFCChildren = []string{"DPUService", "DPUServiceInterface", "DPUServiceChain"}

var deploymentOwnedKinds = []string{
	"DPUDeployment",
	"DPUServiceTemplate",
	"DPUServiceConfiguration",
}

// TestSFCMappingIsDirectOwnership is the written resource graph, as a test.
// Every generated CR must have a name expression and inherit the spec-level
// namespace. Adding DPUDeployment here would give two controllers the same
// DPF children.
func TestSFCMappingIsDirectOwnership(t *testing.T) {
	spec, err := LoadFile(filepath.Join(repoRoot(t), "config", "mappings", "servicefunctionchain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Defaults == nil || spec.Defaults.Namespace.empty() {
		t.Fatal("servicefunctionchain mapping has no defaults.namespace; generated CRs would have no namespace")
	}

	saw := map[string]bool{}
	for i, emit := range spec.Emit {
		kind := emit.Target.Kind
		for _, forbidden := range deploymentOwnedKinds {
			if kind == forbidden {
				t.Errorf("emit[%d] writes %s; DPF's DPUDeployment controller would also own "+
					"the resulting DPUService/Interface/Chain", i, kind)
			}
		}
		if !slices.Contains(directSFCChildren, kind) {
			t.Errorf("emit[%d] kind %s is not in the direct ownership graph %v", i, kind, directSFCChildren)
		}
		if emit.Name.empty() {
			t.Errorf("emit[%d] %s has no name; the resource graph requires one", i, kind)
		}
		if !emit.Namespace.empty() {
			t.Errorf("emit[%d] %s overrides namespace; children must share defaults.namespace "+
				"so one owner and one namespace apply", i, kind)
		}
		saw[kind] = true
	}
	for _, want := range directSFCChildren {
		if !saw[want] {
			t.Errorf("resource graph is missing %s (owner ServiceFunctionChain, namespace defaults.namespace)", want)
		}
	}
}

// TestNoTwoMappingsOwnTheSameDPFKind is the other half of "never two
// controllers": DataProcessingUnit owns provisioning CRs, ServiceFunctionChain
// owns the direct svc.dpu.nvidia.com trio. Crossing those groups is how a
// DPUDeployment-led SFC mapping would fight the DPU mapping for BFB/Flavor.
func TestNoTwoMappingsOwnTheSameDPFKind(t *testing.T) {
	specs, err := LoadDir(filepath.Join(repoRoot(t), "config", "mappings"))
	if err != nil {
		t.Fatal(err)
	}
	type claim struct{ group, kind string }
	owners := map[claim]string{}
	for _, s := range specs {
		for _, emit := range s.Emit {
			k := claim{group: emit.Target.Group, kind: emit.Target.Kind}
			if prev, dup := owners[k]; dup && prev != s.Metadata.Name {
				t.Errorf("mappings %q and %q both emit %s/%s; two controllers would own the same children",
					prev, s.Metadata.Name, emit.Target.Group, emit.Target.Kind)
			}
			owners[k] = s.Metadata.Name
		}
	}
}

// TestSFCApplyEmitsOwnedGraph applies the shipped mapping to a chart SFC and
// checks name + namespace + kind for every generated CR.
func TestSFCApplyEmitsOwnedGraph(t *testing.T) {
	spec, err := LoadFile(filepath.Join(repoRoot(t), "config", "mappings", "servicefunctionchain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := object(
		map[string]any{"name": "hbn-chain", "namespace": "default"},
		map[string]any{
			"networkFunctions": []any{
				map[string]any{"name": "legacy-fw", "image": "nginx:latest"},
				map[string]any{"name": testNFName, "chart": map[string]any{
					"repository": "https://helm.ngc.nvidia.com/nvidia/doca",
					"name":       testNFName,
					"version":    "v26.4.0",
				}},
			},
		},
	)
	objs, err := Apply(spec, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 3 {
		t.Fatalf("got %d objects, want 3 (DPUService + Interface + Chain; image NF skipped)", len(objs))
	}

	want := map[string]struct{ kind, ns string }{
		testNFName:          {kind: "DPUService", ns: "default"},
		testNFName + "-ifc": {kind: "DPUServiceInterface", ns: "default"},
		"hbn-chain":         {kind: "DPUServiceChain", ns: "default"},
	}
	for _, o := range objs {
		exp, ok := want[o.GetName()]
		if !ok {
			t.Errorf("unexpected child %s/%s", o.GetKind(), o.GetName())
			continue
		}
		if o.GetKind() != exp.kind {
			t.Errorf("%s kind=%s, want %s", o.GetName(), o.GetKind(), exp.kind)
		}
		if o.GetNamespace() != exp.ns {
			t.Errorf("%s namespace=%s, want %s", o.GetName(), o.GetNamespace(), exp.ns)
		}
		if o.GetKind() == "DPUServiceInterface" {
			labels := nested(o.Object, "spec", "template", "spec", "template", "metadata", "labels")
			m, _ := labels.(map[string]any)
			if m["service"] != testNFName {
				t.Errorf("interface labels=%v, want service=%s", labels, testNFName)
			}
		}
		if o.GetKind() == "DPUServiceChain" {
			switches := nested(o.Object, "spec", "template", "spec", "template", "spec", "switches")
			list, ok := switches.([]any)
			if !ok || len(list) != 1 {
				t.Errorf("chain switches=%T %#v, want one switch", switches, switches)
			}
		}
		delete(want, o.GetName())
	}
	for name, exp := range want {
		t.Errorf("missing generated CR kind=%s name=%s namespace=%s", exp.kind, name, exp.ns)
	}

	// Cluster-scoped fallback: no source namespace → dpf-operator-system.
	cluster := object(
		map[string]any{"name": "hbn-chain"},
		map[string]any{
			"networkFunctions": []any{
				map[string]any{"name": testNFName, "chart": map[string]any{
					"repository": "https://helm.ngc.nvidia.com/nvidia/doca",
					"name":       testNFName,
					"version":    "v26.4.0",
				}},
			},
		},
	)
	clusterObjs, err := Apply(spec, cluster)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range clusterObjs {
		if o.GetNamespace() != "dpf-operator-system" {
			t.Errorf("cluster-scoped source: %s namespace=%s, want dpf-operator-system",
				o.GetKind(), o.GetNamespace())
		}
	}

	// Image-only SFC emits nothing, including no empty chain.
	imageOnly := object(
		map[string]any{"name": "img-chain", "namespace": "default"},
		map[string]any{
			"networkFunctions": []any{
				map[string]any{"name": "legacy-fw", "image": "nginx:latest"},
			},
		},
	)
	empty, err := Apply(spec, imageOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("image-only SFC emitted %d objects, want 0", len(empty))
	}
}
