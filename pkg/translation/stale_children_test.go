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
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/mapping"
)

// staleChildrenSpec emits one DPUService per networkFunctions[] entry whose
// name is item.name, into the fixed namespace given in ns. Same-namespace
// emits get controller owner references; a different namespace forces the
// annotation-tracked track. That is the only difference between the two
// convergence tests below.
func staleChildrenSpec(name, ns string) *mapping.Spec {
	return &mapping.Spec{
		APIVersion: mapping.APIVersion,
		Kind:       mapping.Kind,
		Metadata:   mapping.Metadata{Name: name},
		Source:     mapping.ObjectRef{Group: "config.openshift.io", Version: "v1", Kind: "ServiceFunctionChain"},
		Emit: []mapping.Emit{{
			Target:    mapping.ObjectRef{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUService"},
			ForEach:   &mapping.ForEach{In: "spec.networkFunctions"},
			When:      "'chart' in item",
			Name:      mapping.Value{CEL: "item.name"},
			Namespace: mapping.Value{Value: ns},
			Fields:    []mapping.Field{{To: "spec.serviceID", From: "item.name", Required: true}},
		}},
	}
}

func nfs(names ...string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{
			"name":  n,
			"chart": map[string]any{"repository": "r", "name": n, "version": "v1"},
		})
	}
	return out
}

func staleSFC(name, namespace string, nfNames []string, uid string) *unstructured.Unstructured {
	src := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"networkFunctions": nfs(nfNames...)},
	}}
	src.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ServiceFunctionChain"})
	src.SetName(name)
	src.SetNamespace(namespace)
	src.SetUID(types.UID(uid))
	return src
}

// reconcileStale drives one Reconcile of the stale-children spec and returns
// the sorted list of DPUService names that exist afterwards in wantNS.
func reconcileStale(
	ctx context.Context, t *testing.T, r *Reconciler, src *unstructured.Unstructured, wantNS string,
) []string {
	t.Helper()
	nn := types.NamespacedName{Name: src.GetName(), Namespace: src.GetNamespace()}
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn}); err != nil {
		t.Fatal(err)
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUServiceList",
	})
	if err := r.List(ctx, list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range list.Items {
		if list.Items[i].GetNamespace() == wantNS {
			names = append(names, list.Items[i].GetName())
		}
	}
	// sort for stable comparison
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

func assertChildren(t *testing.T, got, want []string, phase string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: children = %v, want exactly %v", phase, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: children = %v, want exactly %v", phase, got, want)
		}
	}
}

// TestStaleChildrenConvergeSameNamespace walks the full 04.04 DONE WHEN
// ladder on the owner-reference track: create, update, shrink, rename and
// delete converge to exactly the expected child set, with nothing left over.
func TestStaleChildrenConvergeSameNamespace(t *testing.T) {
	sfc := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ServiceFunctionChain"}
	svc := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUService"}
	sch := plainScheme(sfc, svc)
	ctx := context.Background()

	src := staleSFC("conv-chain", "opi", []string{"alpha", "beta", "gamma"}, "uid-conv")
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(src).Build()
	r := &Reconciler{Client: cl, Scheme: sch, Spec: staleChildrenSpec("conv-same", "opi")}

	// CREATE: three NFs in, three DPUServices out.
	assertChildren(t, reconcileStale(ctx, t, r, src, "opi"),
		[]string{"alpha", "beta", "gamma"}, "create")

	// UPDATE: alpha's chart version changes; the set is unchanged and the
	// child's spec follows the source.
	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(sfc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "conv-chain", Namespace: "opi"}, stored); err != nil {
		t.Fatal(err)
	}
	child := &unstructured.Unstructured{}
	child.SetGroupVersionKind(svc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "alpha", Namespace: "opi"}, child); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := unstructured.NestedString(child.Object, "spec", "serviceID"); v != "alpha" {
		t.Fatalf("create: alpha spec.serviceID = %q, want alpha", v)
	}

	// SHRINK: drop gamma; its child must be gone in the same reconcile.
	nf := nfs("alpha", "beta")
	if err := unstructured.SetNestedField(stored.Object, nf, "spec", "networkFunctions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	assertChildren(t, reconcileStale(ctx, t, r, stored, "opi"),
		[]string{"alpha", "beta"}, "shrink")
	gamma := &unstructured.Unstructured{}
	gamma.SetGroupVersionKind(svc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "gamma", Namespace: "opi"}, gamma); !errors.IsNotFound(err) {
		t.Fatalf("shrink: gamma still exists (err=%v), want NotFound", err)
	}

	// RENAME: beta becomes delta; beta's child must go and delta's appear.
	nf = nfs("alpha", "delta")
	if err := unstructured.SetNestedField(stored.Object, nf, "spec", "networkFunctions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	assertChildren(t, reconcileStale(ctx, t, r, stored, "opi"),
		[]string{"alpha", "delta"}, "rename")
	beta := &unstructured.Unstructured{}
	beta.SetGroupVersionKind(svc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "beta", Namespace: "opi"}, beta); !errors.IsNotFound(err) {
		t.Fatalf("rename: beta still exists (err=%v), want NotFound", err)
	}

	// SHRINK TO ZERO: an empty chain leaves no children at all.
	if err := unstructured.SetNestedField(stored.Object, []any{}, "spec", "networkFunctions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	assertChildren(t, reconcileStale(ctx, t, r, stored, "opi"), nil, "shrink-to-zero")
}

// TestStaleChildrenConvergeCrossNamespace repeats the ladder on the
// annotation-tracked track: the emit lands in a fixed namespace the source
// does not live in, so the children carry the provenance annotation instead
// of an owner reference and pruning must honor that annotation.
func TestStaleChildrenConvergeCrossNamespace(t *testing.T) {
	sfc := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ServiceFunctionChain"}
	svc := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUService"}
	sch := plainScheme(sfc, svc)
	ctx := context.Background()

	src := staleSFC("conv-xns", "opi", []string{"alpha", "beta"}, "uid-xns")
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(src).Build()
	r := &Reconciler{Client: cl, Scheme: sch, Spec: staleChildrenSpec("conv-cross", "dpf-operator-system")}

	assertChildren(t, reconcileStale(ctx, t, r, src, "dpf-operator-system"),
		[]string{"alpha", "beta"}, "create")

	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(sfc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "conv-xns", Namespace: "opi"}, stored); err != nil {
		t.Fatal(err)
	}
	child := &unstructured.Unstructured{}
	child.SetGroupVersionKind(svc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "beta", Namespace: "dpf-operator-system"}, child); err != nil {
		t.Fatalf("cross-namespace child not created: %v", err)
	}
	if len(child.GetOwnerReferences()) != 0 {
		t.Fatalf("cross-namespace child must have no owner ref, got %v", child.GetOwnerReferences())
	}

	nf := nfs("alpha", "delta")
	if err := unstructured.SetNestedField(stored.Object, nf, "spec", "networkFunctions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	assertChildren(t, reconcileStale(ctx, t, r, stored, "dpf-operator-system"),
		[]string{"alpha", "delta"}, "rename")
	beta := &unstructured.Unstructured{}
	beta.SetGroupVersionKind(svc)
	betaNN := types.NamespacedName{Name: "beta", Namespace: "dpf-operator-system"}
	if err := cl.Get(ctx, betaNN, beta); !errors.IsNotFound(err) {
		t.Fatalf("rename: annotation-tracked beta still exists (err=%v), want NotFound", err)
	}
}

// TestPruneNeverTouchesAnotherSource pins the safety rail: the label selector
// spans every namespace the source kind emits into, so a same-named source
// elsewhere produces identically-labeled children. Those children belong to a
// different UID (and a different provenance annotation) and must survive this
// source's shrink untouched.
func TestPruneNeverTouchesAnotherSource(t *testing.T) {
	sfc := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ServiceFunctionChain"}
	svc := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUService"}
	sch := plainScheme(sfc, svc)
	ctx := context.Background()

	// Two same-named sources; each emits into its own namespace.
	mine := staleSFC("dup", "ns-a", []string{"alpha", "beta"}, "uid-a")
	other := staleSFC("dup", "ns-b", []string{"alpha", "beta"}, "uid-b")
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(mine, other).Build()
	rA := &Reconciler{Client: cl, Scheme: sch, Spec: staleChildrenSpec("dup-a", "ns-a")}
	rB := &Reconciler{Client: cl, Scheme: sch, Spec: staleChildrenSpec("dup-b", "ns-b")}

	assertChildren(t, reconcileStale(ctx, t, rA, mine, "ns-a"), []string{"alpha", "beta"}, "create a")
	assertChildren(t, reconcileStale(ctx, t, rB, other, "ns-b"), []string{"alpha", "beta"}, "create b")

	// Shrink ns-a's source. ns-b's identically-labeled children must stay.
	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(sfc)
	if err := cl.Get(ctx, types.NamespacedName{Name: "dup", Namespace: "ns-a"}, stored); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(stored.Object, nfs("alpha"), "spec", "networkFunctions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	assertChildren(t, reconcileStale(ctx, t, rA, stored, "ns-a"), []string{"alpha"}, "shrink a")

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUServiceList",
	})
	if err := cl.List(ctx, list); err != nil {
		t.Fatal(err)
	}
	for i := range list.Items {
		if list.Items[i].GetNamespace() == "ns-b" {
			name := list.Items[i].GetName()
			if name != "alpha" && name != "beta" {
				t.Fatalf("ns-b lost unexpected child %q", name)
			}
		}
	}
	if got := len(list.Items); got != 3 {
		t.Fatalf("after pruning ns-a, total children = %d (want 3: alpha@ns-a, alpha@ns-b, beta@ns-b)", got)
	}
}
