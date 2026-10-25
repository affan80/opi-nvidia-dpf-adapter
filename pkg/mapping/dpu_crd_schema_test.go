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
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

// TestNVIDIASourceEmitsSchemaValidDPFResources is the DPU mapping gate against
// the pinned DPF v26.4 CRDs. Path existence is not enough: the objects Apply
// builds must satisfy required fields, enums, patterns, and the CEL rules the
// OpenAPI schema does not express.
func TestNVIDIASourceEmitsSchemaValidDPFResources(t *testing.T) {
	root := repoRoot(t)
	pin := loadPinnedVersions(t)
	if pin.DPF.Release != "v26.4.0" {
		t.Fatalf("this check is written against DPF v26.4.0 fixtures, pin is %s", pin.DPF.Release)
	}
	spec, err := LoadFile(filepath.Join(root, "config", "mappings", "dataprocessingunit.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	crds := loadPinnedDPFCRDs(t, filepath.Join(root, pin.DPF.Fixtures))

	objs, err := Apply(spec, blueFieldSource(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 4 {
		t.Fatalf("supported BlueField source must emit DPUDevice, DPUFlavor, BFB, and DPU, got %d", len(objs))
	}

	byKind := map[string]map[string]any{}
	for _, obj := range objs {
		kind := obj.GetKind()
		crd, ok := crds[kind]
		if !ok {
			t.Fatalf("emitted %s has no pinned DPF CRD", kind)
		}
		if obj.GetAPIVersion() != "provisioning.dpu.nvidia.com/v1alpha1" {
			t.Errorf("%s apiVersion %s, want provisioning.dpu.nvidia.com/v1alpha1", kind, obj.GetAPIVersion())
		}
		if crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
			t.Errorf("%s CRD scope %s, want Namespaced", kind, crd.Spec.Scope)
		}
		if obj.GetNamespace() != "dpf-operator-system" {
			t.Errorf("%s namespace %q, cluster-scoped OPI source must land in dpf-operator-system", kind, obj.GetNamespace())
		}
		if obj.GetName() == "" {
			t.Errorf("%s name is empty", kind)
		}
		validateAgainstCRD(t, kind, obj.Object, crd)
		byKind[kind] = obj.Object
	}
	for _, kind := range []string{"DPUDevice", "DPUFlavor", "BFB", "DPU"} {
		if byKind[kind] == nil {
			t.Errorf("missing emitted %s", kind)
		}
	}

	// CEL on the v26.4 DPU CRD: exactly one of bfb or blueFieldSoftware.
	dpu := byKind["DPU"]
	if nested(dpu, "spec", "bfb") == nil || nested(dpu, "spec", "blueFieldSoftware") != nil {
		t.Errorf("DPU must set spec.bfb and leave spec.blueFieldSoftware empty, got %#v", nested(dpu, "spec"))
	}
	// CEL: exactly one nodeEffect arm. noEffect=true is that arm.
	if nested(dpu, "spec", "nodeEffect", "noEffect") != true {
		t.Errorf("DPU spec.nodeEffect.noEffect = %#v, want true", nested(dpu, "spec", "nodeEffect", "noEffect"))
	}
	// CEL: device '*' must be the only nvconfig entry.
	nv, _ := nested(byKind["DPUFlavor"], "spec", "nvconfig").([]any)
	if len(nv) != 1 {
		t.Fatalf("DPUFlavor nvconfig len %d, want 1", len(nv))
	}
	entry, _ := nv[0].(map[string]any)
	if entry["device"] != "*" {
		t.Errorf("DPUFlavor nvconfig device %#v, want *", entry["device"])
	}
}

func TestNonBlueFieldSourceEmitsNoDPFResources(t *testing.T) {
	spec, err := LoadFile(mappingDir + "/dataprocessingunit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := blueFieldSource(nil)
	src["spec"].(map[string]any)["dpuProductName"] = "DSC-200"
	objs, err := Apply(spec, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Fatalf("non-BlueField source must not emit DPF resources, got %d", len(objs))
	}
}

func loadPinnedDPFCRDs(t *testing.T, dir string) map[string]apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	out := map[string]apiextensionsv1.CustomResourceDefinition{}
	for _, name := range []string{
		"provisioning.dpu.nvidia.com_dpudevices.yaml",
		"provisioning.dpu.nvidia.com_dpuflavors.yaml",
		"provisioning.dpu.nvidia.com_bfbs.yaml",
		"provisioning.dpu.nvidia.com_dpus.yaml",
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(b, &crd); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[crd.Spec.Names.Kind] = crd
	}
	return out
}

func validateAgainstCRD(t *testing.T, kind string, obj map[string]any, crd apiextensionsv1.CustomResourceDefinition) {
	t.Helper()
	var props *apiextensionsv1.JSONSchemaProps
	for i := range crd.Spec.Versions {
		ver := &crd.Spec.Versions[i]
		if ver.Storage && ver.Name == "v1alpha1" {
			props = ver.Schema.OpenAPIV3Schema
		}
	}
	if props == nil {
		t.Fatalf("%s: pinned CRD has no storage v1alpha1 schema", kind)
	}
	var internal apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
		props, &internal, nil,
	); err != nil {
		t.Fatal(err)
	}
	validator, _, err := validation.NewSchemaValidator(&internal)
	if err != nil {
		t.Fatalf("%s: schema validator: %v", kind, err)
	}
	errs := validation.ValidateCustomResource(field.NewPath(kind), obj, validator)
	if len(errs) > 0 {
		t.Errorf("%s is not schema-valid against pinned DPF v26.4.0:\n%s", kind, errs.ToAggregate())
	}
}
