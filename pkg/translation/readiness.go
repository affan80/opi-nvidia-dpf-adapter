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
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/lifecycle"
)

// DPFNotReadyRequeue is the delay used while the LCM has not reported
// VendorOperatorReady, or while a target CRD is still absent. It is long
// enough that a missing API does not spin the worker, and short enough
// that a missed watch still converges. A CRD add or VendorOperatorReady
// transition enqueues the source immediately and does not wait this out.
const DPFNotReadyRequeue = 15 * time.Second

func lcmConfigGVK() schema.GroupVersionKind {
	return lifecycle.DpuOperatorConfigGVK
}

func crdGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
}

// lcmBlocks is true when a DpuOperatorConfig exists and none of them
// report VendorOperatorReady=True. No config, or a scheme that does not
// know the type, does not block: unit tests and a translator-only run
// have nothing to wait for. Latest contract: docs/lcm-operator-status.md.
func (r *Reconciler) lcmBlocks(ctx context.Context) (bool, error) {
	if r.Scheme == nil || !r.Scheme.Recognizes(lcmConfigGVK()) {
		return false, nil
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   lcmConfigGVK().Group,
		Version: lcmConfigGVK().Version,
		Kind:    lcmConfigGVK().Kind + "List",
	})
	if err := r.List(ctx, list); err != nil {
		if apimeta.IsNoMatchError(err) {
			return false, nil
		}
		return false, err
	}
	if len(list.Items) == 0 {
		return false, nil
	}
	for i := range list.Items {
		if lcmConditionReady(&list.Items[i]) {
			return false, nil
		}
	}
	return true, nil
}

func lcmConditionReady(obj *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, raw := range conds {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == lifecycle.OperatorReadyConditionType &&
			cond["status"] == string(metav1.ConditionTrue) {
			return true
		}
	}
	return false
}

// MapperTargetsInstalled is the production TargetsInstalled hook. A miss
// resets a discovery mapper once and looks up again, so a CRD the LCM
// just installed is visible in this reconcile. A second miss returns
// not-ready; the caller requeues on a fixed delay instead of looping.
func MapperTargetsInstalled(mapper apimeta.RESTMapper) func(context.Context, []schema.GroupVersionKind) (bool, error) {
	return func(_ context.Context, gvks []schema.GroupVersionKind) (bool, error) {
		if mapper == nil || len(gvks) == 0 {
			return true, nil
		}
		missing, err := mappingMisses(mapper, gvks)
		if err != nil {
			return false, err
		}
		if !missing {
			return true, nil
		}
		reset, ok := mapper.(apimeta.ResettableRESTMapper)
		if !ok {
			return false, nil
		}
		reset.Reset()
		missing, err = mappingMisses(mapper, gvks)
		if err != nil {
			return false, err
		}
		return !missing, nil
	}
}

func mappingMisses(mapper apimeta.RESTMapper, gvks []schema.GroupVersionKind) (bool, error) {
	missing := false
	for _, gvk := range gvks {
		_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err == nil {
			continue
		}
		if apimeta.IsNoMatchError(err) {
			missing = true
			continue
		}
		return false, err
	}
	return missing, nil
}

// ownableTargets drops kinds discovery does not serve yet. Owning them
// makes the manager wait for a cache that cannot sync.
func ownableTargets(mapper apimeta.RESTMapper, gvks []schema.GroupVersionKind) ([]schema.GroupVersionKind, error) {
	if mapper == nil {
		return gvks, nil
	}
	var out []schema.GroupVersionKind
	for _, gvk := range gvks {
		_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if apimeta.IsNoMatchError(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, gvk)
	}
	return out, nil
}

func uniqueGVKs(objs []*unstructured.Unstructured) []schema.GroupVersionKind {
	seen := map[schema.GroupVersionKind]bool{}
	var out []schema.GroupVersionKind
	for _, obj := range objs {
		gvk := obj.GroupVersionKind()
		if seen[gvk] {
			continue
		}
		seen[gvk] = true
		out = append(out, gvk)
	}
	return out
}

func (r *Reconciler) enqueueIfLCMReady(ctx context.Context, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !lcmConditionReady(u) {
		return nil
	}
	return r.sourceRequests(ctx)
}

func (r *Reconciler) enqueueIfTargetCRD(ctx context.Context, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !r.crdMatchesTarget(u) {
		return nil
	}
	return r.sourceRequests(ctx)
}

func (r *Reconciler) crdMatchesTarget(crd *unstructured.Unstructured) bool {
	group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
	kind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	for _, emit := range r.Spec.Emit {
		gvk := emit.Target.GVK()
		if gvk.Group != group || gvk.Kind != kind {
			continue
		}
		for _, raw := range versions {
			ver, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := ver["name"].(string)
			served, _ := ver["served"].(bool)
			if name == gvk.Version && served {
				return true
			}
		}
	}
	return false
}

func (r *Reconciler) sourceRequests(ctx context.Context) []reconcile.Request {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   r.Spec.Source.Group,
		Version: r.Spec.Source.Version,
		Kind:    r.Spec.Source.Kind + "List",
	})
	if err := r.List(ctx, list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      list.Items[i].GetName(),
			Namespace: list.Items[i].GetNamespace(),
		}})
	}
	return out
}
