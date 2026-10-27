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

	"sigs.k8s.io/yaml"
)

func TestMainLeaderElectionReleaseOnCancel(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	lines := strings.Split(src, "\n")
	found := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, "LeaderElectionReleaseOnCancel: true") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cmd/main.go must enable LeaderElectionReleaseOnCancel: true for fast leader failover")
	}
}

func TestManagerDeploymentReplicasAndElection(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(string(b), "---")
	var deploymentYAML string
	for _, p := range parts {
		if strings.Contains(p, "kind: Deployment") {
			deploymentYAML = p
			break
		}
	}
	if deploymentYAML == "" {
		t.Fatal("kind: Deployment not found in config/manager/manager.yaml")
	}

	var doc struct {
		Spec struct {
			Replicas int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Args []string `json:"args"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal([]byte(deploymentYAML), &doc); err != nil {
		t.Fatalf("failed to parse Deployment YAML: %v", err)
	}

	if doc.Spec.Replicas < 2 {
		t.Fatalf("Deployment replicas = %d, want >= 2 for active-standby singleton with failover", doc.Spec.Replicas)
	}

	enabled := false
	for _, c := range doc.Spec.Template.Spec.Containers {
		for _, arg := range c.Args {
			switch {
			case arg == "--leader-elect=false" || arg == "--leader-elect=0":
				t.Fatalf("Deployment arg %q disables election while replicas=%d; two writers would share DpuOperatorConfig",
					arg, doc.Spec.Replicas)
			case arg == "--leader-elect" || arg == "--leader-elect=true":
				enabled = true
			}
		}
	}
	if !enabled {
		t.Fatal("Deployment container args must include --leader-elect so only one replica is the active controller")
	}
}

func TestMainWiresLeaderElectionLease(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `LeaderElection:         enableLeaderElection`) &&
		!strings.Contains(src, `LeaderElection: enableLeaderElection`) {
		t.Fatal("cmd/main.go must pass --leader-elect into ctrl.Options.LeaderElection")
	}
	if !strings.Contains(src, `LeaderElectionID:       "9ab915d2.opi.nvidia.com"`) &&
		!strings.Contains(src, `LeaderElectionID: "9ab915d2.opi.nvidia.com"`) {
		t.Fatal("cmd/main.go must pin LeaderElectionID so both replicas contest the same lease")
	}
}
