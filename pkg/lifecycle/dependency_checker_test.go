/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    10|Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

func TestCheckDependenciesReadyCluster(t *testing.T) {
	got := CheckDependencies(context.Background(), ReadyClusterProbe(), VendorNVIDIA)
	if !got.OK || len(got.Missing) != 0 {
		t.Fatalf("ready cluster: %+v", got)
	}
	if got.Kubernetes != SupportedKubernetesVersion {
		t.Fatalf("kubernetes = %q", got.Kubernetes)
	}
}

func TestCheckDependenciesBlocksMissingArgo(t *testing.T) {
	probe := StaticProbe{
		Version: SupportedKubernetesVersion,
		Present: map[string]bool{
			apiKey(certManagerGroup, certManagerKind): true,
		},
	}
	got := CheckDependencies(context.Background(), probe, VendorNVIDIA)
	if got.OK {
		t.Fatal("missing Argo CD must not report success")
	}
	if len(got.Missing) != 1 || got.Missing[0] != DepArgoCD {
		t.Fatalf("missing = %v, want [%s]", got.Missing, DepArgoCD)
	}
	if !strings.Contains(got.Message, DepArgoCD) {
		t.Fatalf("message %q does not name argo-cd", got.Message)
	}
}

func TestCheckDependenciesBlocksMissingCertManager(t *testing.T) {
	probe := StaticProbe{
		Version: SupportedKubernetesVersion,
		Present: map[string]bool{apiKey(argoGroup, argoKind): true},
	}
	got := CheckDependencies(context.Background(), probe, VendorNVIDIA)
	if got.OK || len(got.Missing) != 1 || got.Missing[0] != DepCertManager {
		t.Fatalf("missing cert-manager: %+v", got)
	}
}

func TestCheckDependenciesBlocksUnsupportedKubernetes(t *testing.T) {
	probe := StaticProbe{
		Version: "v1.29.0",
		Present: map[string]bool{
			apiKey(certManagerGroup, certManagerKind): true,
			apiKey(argoGroup, argoKind):               true,
		},
	}
	got := CheckDependencies(context.Background(), probe, VendorNVIDIA)
	if got.OK || len(got.Missing) != 1 || got.Missing[0] != "kubernetes" {
		t.Fatalf("unsupported k8s: %+v", got)
	}
	if !strings.Contains(got.Message, "v1.29.0") || !strings.Contains(got.Message, "v1.36") {
		t.Fatalf("message %q should name both versions", got.Message)
	}
}

func TestCheckDependenciesAcceptsK3sBuildMetadata(t *testing.T) {
	probe := StaticProbe{
		Version: "v1.36.4+k3s1",
		Present: map[string]bool{
			apiKey(certManagerGroup, certManagerKind): true,
			apiKey(argoGroup, argoKind):               true,
		},
	}
	got := CheckDependencies(context.Background(), probe, VendorNVIDIA)
	if !got.OK {
		t.Fatalf("k3s build metadata should match the pinned minor: %+v", got)
	}
}

func TestCheckDependenciesBlocksUnreachableCluster(t *testing.T) {
	probe := StaticProbe{VersionErr: errors.New("connection refused")}
	got := CheckDependencies(context.Background(), probe, VendorNVIDIA)
	if got.OK || len(got.Missing) != 1 || got.Missing[0] != DepCluster {
		t.Fatalf("unreachable: %+v", got)
	}
	if !strings.Contains(got.Message, clusterAccessMsg) {
		t.Fatalf("unreachable message: %q", got.Message)
	}
}

func TestCheckDependenciesNilProbeIsBlocked(t *testing.T) {
	got := CheckDependencies(context.Background(), nil, VendorNVIDIA)
	if got.OK || len(got.Missing) != 1 || got.Missing[0] != DepCluster {
		t.Fatalf("nil probe: %+v", got)
	}
}

func TestCheckDependenciesAMDDoesNotRequireDPFCharts(t *testing.T) {
	probe := StaticProbe{Version: SupportedKubernetesVersion}
	got := CheckDependencies(context.Background(), probe, VendorAMD)
	if !got.OK {
		t.Fatalf("AMD must not require Argo/cert-manager: %+v", got)
	}
}

func TestCheckDependenciesKamajiOnlyWhenApplicable(t *testing.T) {
	base := map[string]bool{
		apiKey(certManagerGroup, certManagerKind): true,
		apiKey(argoGroup, argoKind):               true,
	}
	ok := CheckDependencies(context.Background(), StaticProbe{
		Version: SupportedKubernetesVersion, Present: base,
	}, VendorNVIDIA)
	if !ok.OK {
		t.Fatalf("kamaji not applicable: %+v", ok)
	}

	blocked := CheckDependencies(context.Background(), StaticProbe{
		Version: SupportedKubernetesVersion, Present: base, KamajiNeed: true,
	}, VendorNVIDIA)
	if blocked.OK || len(blocked.Missing) != 1 || blocked.Missing[0] != DepKamaji {
		t.Fatalf("kamaji required: %+v", blocked)
	}

	present := map[string]bool{
		apiKey(certManagerGroup, certManagerKind): true,
		apiKey(argoGroup, argoKind):               true,
		apiKey(kamajiGroup, kamajiKind):           true,
	}
	ready := CheckDependencies(context.Background(), StaticProbe{
		Version: SupportedKubernetesVersion, Present: present, KamajiNeed: true,
	}, VendorNVIDIA)
	if !ready.OK {
		t.Fatalf("kamaji present: %+v", ready)
	}
}

func nvidiaReadyAPIs() map[string]bool {
	return map[string]bool{
		apiKey(certManagerGroup, certManagerKind): true,
		apiKey(argoGroup, argoKind):               true,
	}
}

// TestEnsureInstalledBlockedCases is the done-when: a missing prerequisite
// cannot look like a successful install. The installer must not run.
func TestEnsureInstalledBlockedCases(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, "v26.4.0")
	detected := InstallResult{Action: InstallDetected, Present: true, Version: "v26.4.0"}
	cases := []struct {
		name    string
		probe   ClusterProbe
		wantMsg string
	}{
		{
			"missing Argo CD",
			StaticProbe{
				Version: SupportedKubernetesVersion,
				Present: map[string]bool{apiKey(certManagerGroup, certManagerKind): true},
			},
			DepArgoCD,
		},
		{
			"missing cert-manager",
			StaticProbe{
				Version: SupportedKubernetesVersion,
				Present: map[string]bool{apiKey(argoGroup, argoKind): true},
			},
			DepCertManager,
		},
		{
			"Kubernetes v1.29",
			StaticProbe{Version: "v1.29.0", Present: nvidiaReadyAPIs()},
			"v1.29.0",
		},
		{
			"unreachable API",
			StaticProbe{VersionErr: errors.New("connection refused")},
			clusterAccessMsg,
		},
		{
			"nil probe",
			nil,
			"no cluster probe",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingInstaller{result: detected}
			out, err := (&LifecycleManager{Installer: rec, Probe: tc.probe}).
				EnsureInstalled(context.Background(), obj)
			if err != nil {
				t.Fatal(err)
			}
			if out.Policy.Skew != SkewBlocked {
				t.Fatalf("skew = %s, want Blocked", out.Policy.Skew)
			}
			if out.Install.Action != InstallSkipped {
				t.Fatalf("action = %s, want skipped (no false detected)", out.Install.Action)
			}
			if out.Readiness.Ready {
				t.Fatal("AssessReadiness must be false")
			}
			if len(rec.calls) != 0 {
				t.Fatalf("installer ran %d times; missing prerequisite must skip it", len(rec.calls))
			}
			if !strings.Contains(out.Policy.Message, tc.wantMsg) {
				t.Fatalf("message %q, want substring %q", out.Policy.Message, tc.wantMsg)
			}
		})
	}
}

func TestReconcilePublishesBlockedCondition(t *testing.T) {
	sch := lcmScheme()
	cases := []struct {
		name    string
		probe   ClusterProbe
		wantMsg string
	}{
		{
			"missing Argo CD and cert-manager",
			StaticProbe{Version: SupportedKubernetesVersion},
			DepArgoCD,
		},
		{
			"Kubernetes v1.29",
			StaticProbe{Version: "v1.29.0", Present: nvidiaReadyAPIs()},
			"v1.29.0",
		},
		{
			"unreachable API",
			StaticProbe{VersionErr: errors.New("connection refused")},
			clusterAccessMsg,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := configWithVersion(VendorNVIDIA, "v26.4.0")
			cl := fake.NewClientBuilder().WithScheme(sch).
				WithObjects(obj).WithStatusSubresource(obj).Build()
			r := &LifecycleManager{
				Client: cl, Scheme: sch,
				Installer: &recordingInstaller{result: InstallResult{
					Action: InstallDetected, Present: true, Version: "v26.4.0",
				}},
				Probe: tc.probe,
			}
			if _, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "cfg"},
			}); err != nil {
				t.Fatal(err)
			}
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(DpuOperatorConfigGVK)
			if err := cl.Get(context.Background(), types.NamespacedName{Name: "cfg"}, got); err != nil {
				t.Fatal(err)
			}
			cond := conditionOfType(t, got, SupportedConditionType)
			if cond == nil {
				t.Fatal("no VendorOperatorSupported condition")
			}
			if cond["status"] != "False" || cond["reason"] != string(SkewBlocked) {
				t.Fatalf("got %v/%v, want False/Blocked", cond["status"], cond["reason"])
			}
			msg, _ := cond["message"].(string)
			if !strings.Contains(msg, tc.wantMsg) {
				t.Fatalf("message %q, want substring %q", msg, tc.wantMsg)
			}
			if c := conditionOfType(t, got, "Ready"); c != nil {
				t.Fatalf("LCM wrote Ready: %v", c)
			}
		})
	}
}

func TestEnsureInstalledRunsWhenDependenciesPresent(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, "v26.4.0")
	rec := &recordingInstaller{result: InstallResult{
		Action: InstallDetected, Present: true, Version: "v26.4.0",
	}}
	r := &LifecycleManager{Installer: rec, Probe: ReadyClusterProbe()}
	out, err := r.EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("installer calls = %d, want 1", len(rec.calls))
	}
	if out.Policy.Skew != SkewSupported || !out.Readiness.Ready {
		t.Fatalf("got skew=%s ready=%v", out.Policy.Skew, out.Readiness.Ready)
	}
}

func TestDependencyCheckerNeverMutates(t *testing.T) {
	b, err := os.ReadFile("dependency_checker.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"helm.New", "exec.Command(\"helm\"", "HelmClient"} {
		if strings.Contains(src, banned) {
			t.Errorf("dependency_checker.go contains %q", banned)
		}
	}
}

func TestSupportedKubernetesMatchesVersionsYAML(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		Kubernetes struct {
			Version string `json:"version"`
		} `json:"kubernetes"`
		DPF struct {
			Dependencies struct {
				Required    []string `json:"required"`
				Conditional []string `json:"conditional"`
			} `json:"dependencies"`
		} `json:"dpf"`
	}
	if err := yaml.Unmarshal(b, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Kubernetes.Version != SupportedKubernetesVersion {
		t.Fatalf("versions.yaml kubernetes %s != %s",
			pin.Kubernetes.Version, SupportedKubernetesVersion)
	}
	wantReq := []string{DepCertManager, DepArgoCD}
	if strings.Join(pin.DPF.Dependencies.Required, ",") != strings.Join(wantReq, ",") {
		t.Fatalf("required deps = %v, want %v", pin.DPF.Dependencies.Required, wantReq)
	}
	if len(pin.DPF.Dependencies.Conditional) != 1 ||
		pin.DPF.Dependencies.Conditional[0] != DepKamaji {
		t.Fatalf("conditional deps = %v, want [%s]",
			pin.DPF.Dependencies.Conditional, DepKamaji)
	}
}
