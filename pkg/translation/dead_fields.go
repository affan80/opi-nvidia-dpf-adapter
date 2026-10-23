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
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list

// Live CRDs on dh2 lag the v26.4 fixtures (Helm --take-ownership left the
// 2026-09-07 schemas). Mappings stay aligned to the fixtures. Server-side
// apply against a lagging CRD fails the whole object:
//
//	.spec.security: field not declared in schema
//	strict decoding error: unknown field "spec.vendor"
//
// applyOne drops that path and retries so a single dead field cannot block
// the rest of the graph. This adapter does not upgrade those CRDs.

var (
	undeclaredPath = regexp.MustCompile(`(?i)(?:^|[^\w.])(\.?[A-Za-z][\w.]*)\s*:\s*field not declared in schema`)
	unknownQuoted  = regexp.MustCompile(`(?i)unknown field "([^"]+)"`)
)

func undeclaredFieldPath(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if m := undeclaredPath.FindStringSubmatch(s); len(m) == 2 {
		return normalizeFieldPath(m[1])
	}
	if m := unknownQuoted.FindStringSubmatch(s); len(m) == 2 {
		return normalizeFieldPath(m[1])
	}
	return ""
}

func normalizeFieldPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, ".")
	p = strings.TrimPrefix(p, "/")
	p = strings.ReplaceAll(p, "/", ".")
	return p
}

// dropField deletes a dotted path from an unstructured object. Empty parent
// maps are left in place; a later undeclared-field retry drops them.
func dropField(obj *unstructured.Unstructured, dotted string) bool {
	if obj == nil || obj.Object == nil {
		return false
	}
	parts := strings.Split(dotted, ".")
	if len(parts) == 0 || parts[0] == "" {
		return false
	}
	return dropNestedMap(obj.Object, parts)
}

func dropNestedMap(m map[string]any, parts []string) bool {
	if len(parts) == 1 {
		if _, ok := m[parts[0]]; !ok {
			return false
		}
		delete(m, parts[0])
		return true
	}
	child, ok := m[parts[0]].(map[string]any)
	if !ok {
		return false
	}
	return dropNestedMap(child, parts[1:])
}

func pruneUnknownSpec(obj *unstructured.Unstructured, crdSchema map[string]any) []string {
	if obj == nil || obj.Object == nil || crdSchema == nil {
		return nil
	}
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return nil
	}
	specSchema := nestedSchema(crdSchema, "spec")
	if specSchema == nil {
		return nil
	}
	return pruneMap(spec, specSchema, "spec")
}

func nestedSchema(root map[string]any, name string) map[string]any {
	props, _ := root["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	child, _ := props[name].(map[string]any)
	return child
}

func preserveUnknown(crdSchema map[string]any) bool {
	if crdSchema == nil {
		return false
	}
	if v, ok := crdSchema["x-kubernetes-preserve-unknown-fields"].(bool); ok && v {
		return true
	}
	if _, ok := crdSchema["additionalProperties"]; ok {
		return true
	}
	return false
}

func pruneMap(m map[string]any, crdSchema map[string]any, prefix string) []string {
	if preserveUnknown(crdSchema) {
		return nil
	}
	props, _ := crdSchema["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	var dropped []string
	for k, v := range m {
		childSchema, ok := props[k].(map[string]any)
		path := prefix + "." + k
		if !ok {
			delete(m, k)
			dropped = append(dropped, path)
			continue
		}
		if child, isMap := v.(map[string]any); isMap {
			dropped = append(dropped, pruneMap(child, childSchema, path)...)
		}
	}
	return dropped
}

func (r *Reconciler) liveCRDSchema(ctx context.Context, gvk schema.GroupVersionKind) (map[string]any, error) {
	r.schemaMu.Lock()
	if r.schemas != nil {
		if s, ok := r.schemas[gvk]; ok {
			r.schemaMu.Unlock()
			return s, nil
		}
	}
	r.schemaMu.Unlock()

	mappingREST, err := r.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition",
	})
	name := mappingREST.Resource.Resource + "." + gvk.Group
	if err := r.Get(ctx, client.ObjectKey{Name: name}, crd); err != nil {
		return nil, err
	}
	openapi := storageOpenAPI(crd.Object)
	r.schemaMu.Lock()
	if r.schemas == nil {
		r.schemas = map[schema.GroupVersionKind]map[string]any{}
	}
	r.schemas[gvk] = openapi
	r.schemaMu.Unlock()
	return openapi, nil
}

func storageOpenAPI(crd map[string]any) map[string]any {
	spec, _ := crd["spec"].(map[string]any)
	versions, _ := spec["versions"].([]any)
	for _, raw := range versions {
		v, _ := raw.(map[string]any)
		if v["storage"] != true {
			continue
		}
		sch, _ := v["schema"].(map[string]any)
		root, _ := sch["openAPIV3Schema"].(map[string]any)
		return root
	}
	return nil
}

func omitLiveUnknownFields(obj *unstructured.Unstructured, err error) (string, error) {
	path := undeclaredFieldPath(err)
	if path == "" {
		return "", err
	}
	if !dropField(obj, path) {
		return "", fmt.Errorf("%w (could not drop %s)", err, path)
	}
	return path, nil
}
