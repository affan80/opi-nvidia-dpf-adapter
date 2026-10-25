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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func probeChartRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "lcm-probe")
}

func TestClusterHelmClientInstallUpgradeRollback(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	helm.ChartRoot = probeChartRoot(t)
	ctx := context.Background()

	v1 := ProbeHelmConfig("0.1.0")
	got, err := helm.Install(ctx, v1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity() != HelmProbeNamespace+"/"+HelmProbeReleaseName+"@0.1.0" {
		t.Fatalf("install identity %s", got.Identity())
	}
	if got.Name == NVIDIAReleaseName {
		t.Fatal("probe must not use the live dpf-operator release name")
	}

	cm := &corev1.ConfigMap{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: HelmProbeNamespace, Name: HelmProbeReleaseName}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["version"] != "0.1.0" {
		t.Fatalf("configmap version %q", cm.Data["version"])
	}

	v2 := ProbeHelmConfig("0.2.0")
	up, err := helm.Upgrade(ctx, v2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if up.ChartVersion != "0.2.0" || up.Revision != 2 {
		t.Fatalf("upgrade %+v", up)
	}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: HelmProbeNamespace, Name: HelmProbeReleaseName}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["version"] != "0.2.0" {
		t.Fatalf("upgraded configmap version %q", cm.Data["version"])
	}

	rb, err := helm.Rollback(ctx, v2, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if rb.ChartVersion != "0.1.0" || rb.Status != HelmStatusDeployed {
		t.Fatalf("rollback %+v", rb)
	}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: HelmProbeNamespace, Name: HelmProbeReleaseName}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["version"] != "0.1.0" {
		t.Fatalf("rolled-back configmap version %q", cm.Data["version"])
	}

	if _, err := helm.Uninstall(ctx, v1); err != nil {
		t.Fatal(err)
	}
	if _, err := helm.GetRelease(ctx, HelmProbeNamespace, HelmProbeReleaseName); !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("after uninstall GetRelease = %v", err)
	}
}

func TestClusterHelmClientRefusesLiveDPFName(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	helm.ChartRoot = probeChartRoot(t)
	cfg := mustNVIDIAConfig(t)
	if _, err := helm.Install(context.Background(), cfg, nil); !errors.Is(err, ErrHelmMutationNotConfigured) {
		t.Fatalf("live DPF install = %v, want ErrHelmMutationNotConfigured", err)
	}
}

func TestClusterHelmClientNeedsChartRoot(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	if _, err := helm.Install(context.Background(), ProbeHelmConfig("0.1.0"), nil); !errors.Is(err, ErrHelmMutationNotConfigured) {
		t.Fatalf("empty ChartRoot = %v", err)
	}
}

func TestClusterHelmClientRefusesHelmArgs(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	helm.ChartRoot = probeChartRoot(t)
	cfg := ProbeHelmConfig("0.1.0")
	cfg.HelmArgs = []string{"--debug"}
	if _, err := helm.Install(context.Background(), cfg, nil); err == nil {
		t.Fatal("helm args must be rejected")
	}
}

func TestMainWiresClusterHelmClient(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "lifecycle.NewClusterHelmClient") {
		t.Fatal("cmd/main.go must wire NewClusterHelmClient")
	}
	if strings.Contains(src, "MemoryHelmClient") {
		t.Fatal("cmd/main.go must not use the in-process test Helm client")
	}
}

func TestClusterHelmClientSourceHasNoCLIOrHelmSDK(t *testing.T) {
	b, err := os.ReadFile("cluster_helm_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"helm.New", "exec.Command", "os/exec", "\"helm.sh/helm"} {
		if strings.Contains(src, banned) {
			t.Errorf("cluster_helm_client.go contains %q", banned)
		}
	}
}
