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
	"strings"
	"testing"
)

func TestAPFManifestsLimitLCMServiceAccount(t *testing.T) {
	root := filepath.Join("..", "..", "config", "apf")
	prio, err := os.ReadFile(filepath.Join(root, "prioritylevel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prio), "kind: PriorityLevelConfiguration") ||
		!strings.Contains(string(prio), "type: Limited") {
		t.Fatal("APF priority must be Limited, not an apiserver flag")
	}
	flow, err := os.ReadFile(filepath.Join(root, "flowschema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(flow)
	for _, want := range []string{
		"kind: FlowSchema",
		"dpuoperatorconfigs",
		"opi-nvidia-dpf-adapter-controller-manager",
		"opi-nvidia-dpf-adapter-system",
		"priorityLevelConfiguration",
		"opi-nvidia-lcm",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("flowschema missing %q", want)
		}
	}
	if strings.Contains(src, "systemctl") || strings.Contains(string(prio), "systemctl") {
		t.Fatal("APF manifests must not change the host")
	}
}

func TestWebhookManifestsValidateDpuOperatorConfig(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "webhook", "manifests.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"kind: ValidatingWebhookConfiguration",
		DpuOperatorConfigValidatePath,
		"dpuoperatorconfigs",
		"failurePolicy: Fail",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("webhook manifest missing %q", want)
		}
	}
}
