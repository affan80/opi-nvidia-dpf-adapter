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

package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type rbacRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
}

type clusterRole struct {
	Rules []rbacRule `json:"rules"`
}

func loadManagerRole(t *testing.T) clusterRole {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "config", "rbac", "role.yaml"),
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "..", "..", "config", "rbac", "role.yaml"))
	}
	var b []byte
	var err error
	var path string
	for _, path = range candidates {
		b, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("role.yaml not found in %v: %v", candidates, err)
	}
	var role clusterRole
	if err := yaml.Unmarshal(b, &role); err != nil {
		t.Fatal(err)
	}
	if len(role.Rules) == 0 {
		t.Fatal("manager-role has no rules")
	}
	return role
}

func TestManagerRoleHasNoWildcards(t *testing.T) {
	role := loadManagerRole(t)
	for _, rule := range role.Rules {
		for _, item := range append(append(rule.APIGroups, rule.Resources...), rule.Verbs...) {
			if item == "*" {
				t.Fatalf("wildcard permission: groups=%v resources=%v verbs=%v",
					rule.APIGroups, rule.Resources, rule.Verbs)
			}
		}
	}
}

func TestManagerRoleDoesNotDeleteSharedControlPlane(t *testing.T) {
	role := loadManagerRole(t)
	forbiddenDelete := map[string]bool{
		"dpuoperatorconfigs":            true,
		"customresourcedefinitions":     true,
		"deployments":                   true,
		"dpuoperatorconfigs/status":     true,
		"dpuoperatorconfigs/finalizers": true,
	}
	for _, rule := range role.Rules {
		hasDelete := false
		for _, v := range rule.Verbs {
			if v == "delete" || v == "deletecollection" {
				hasDelete = true
			}
		}
		if !hasDelete {
			continue
		}
		for _, res := range rule.Resources {
			if forbiddenDelete[res] {
				t.Fatalf("delete granted on %s: %+v", res, rule)
			}
		}
	}
}

func TestManagerRoleGrantsLCMObserveVerbs(t *testing.T) {
	role := loadManagerRole(t)
	need := []struct {
		group, resource string
		verbs           []string
	}{
		{"apps", "deployments", []string{"get", "list", "watch"}},
		{"config.openshift.io", "dpuoperatorconfigs", []string{"get", "list", "watch", "update"}},
		{"provisioning.dpu.nvidia.com", "dpuclusters", []string{"get", "list", "watch"}},
		{"operator.dpu.nvidia.com", "dpfoperatorconfigs", []string{"get", "list", "watch"}},
		{"apiextensions.k8s.io", "customresourcedefinitions", []string{"get", "list", "watch"}},
	}
	for _, n := range need {
		if !roleHas(role, n.group, n.resource, n.verbs) {
			t.Errorf("missing %s %s verbs %v", n.group, n.resource, n.verbs)
		}
	}
}

func TestLifecycleMarkersDoNotGrantWildcardOrCRDDelete(t *testing.T) {
	b, err := os.ReadFile("lifecycle_controller.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "verbs=*") || strings.Contains(src, "resources=*") {
		t.Fatal("lifecycle markers use a wildcard")
	}
	if strings.Contains(src, "customresourcedefinitions,verbs=") &&
		strings.Contains(src, "customresourcedefinitions,verbs=get;list;watch;delete") {
		t.Fatal("LCM must not delete CRDs")
	}
	if strings.Contains(src, "dpuoperatorconfigs,verbs=get;list;watch;update;delete") {
		t.Fatal("LCM must not delete DpuOperatorConfig objects")
	}
}

func roleHas(role clusterRole, group, resource string, verbs []string) bool {
	have := map[string]bool{}
	for _, rule := range role.Rules {
		matchGroup := false
		for _, g := range rule.APIGroups {
			if g == group {
				matchGroup = true
			}
		}
		if !matchGroup {
			continue
		}
		matchRes := false
		for _, r := range rule.Resources {
			if r == resource {
				matchRes = true
			}
		}
		if !matchRes {
			continue
		}
		for _, v := range rule.Verbs {
			have[v] = true
		}
	}
	for _, v := range verbs {
		if !have[v] {
			return false
		}
	}
	return true
}
