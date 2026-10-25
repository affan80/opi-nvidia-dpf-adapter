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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestDefaultInstallConfigIsDeterministic(t *testing.T) {
	a, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DefaultInstallConfig(" NVIDIA ")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("defaults drifted: %+v vs %+v", a, b)
	}
	if err := ValidateInstallConfig(a); err != nil {
		t.Fatal(err)
	}
	if a.ChartVersion != nvidiaDPFMin || a.Namespace != DefaultNamespace ||
		a.ChartRepo != NVIDIAChartRepo || a.ReleaseOwner != ReleaseOwner ||
		len(a.HelmArgs) != 0 {
		t.Fatalf("pinned nvidia defaults: %+v", a)
	}
}

func TestDefaultInstallConfigRejectsUnknownVendor(t *testing.T) {
	if _, err := DefaultInstallConfig("acme"); err == nil {
		t.Fatal("acme must not get an install config")
	}
}

func TestValidateInstallConfigRejectsOverrides(t *testing.T) {
	good, _ := DefaultInstallConfig(VendorNVIDIA)
	cases := []struct {
		name string
		mut  func(*InstallConfig)
	}{
		{"chart url", func(c *InstallConfig) { c.ChartRepo = "https://evil.example/charts" }},
		{"file url", func(c *InstallConfig) { c.ChartRepo = "file:///etc/passwd" }},
		{"http", func(c *InstallConfig) { c.ChartRepo = "http://helm.ngc.nvidia.com/nvidia/doca" }},
		{"helm args", func(c *InstallConfig) { c.HelmArgs = []string{"--set", "image=evil"} }},
		{"namespace", func(c *InstallConfig) { c.Namespace = "kube-system" }},
		{"chart version", func(c *InstallConfig) { c.ChartVersion = "v99.0.0" }},
		{"release name", func(c *InstallConfig) { c.ReleaseName = "mine" }},
		{"owner", func(c *InstallConfig) { c.ReleaseOwner = "attacker" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.mut(&c)
			if err := ValidateInstallConfig(c); err == nil {
				t.Fatalf("accepted %+v", c)
			}
		})
	}
}

func TestAMDHasNoHelmChart(t *testing.T) {
	c, err := DefaultInstallConfig(VendorAMD)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstallConfig(c); err != nil {
		t.Fatal(err)
	}
	if c.ChartRepo != "" || c.ChartName != "" {
		t.Fatalf("amd must not pin a helm chart: %+v", c)
	}
	c.ChartRepo = NVIDIAChartRepo
	if err := ValidateInstallConfig(c); err == nil {
		t.Fatal("amd must not carry the nvidia chart")
	}
}

func TestRejectInstallOverrides(t *testing.T) {
	for _, key := range []string{
		"lifecycle.opi.nvidia.com/chart-url",
		helmArgsAnnotation,
		helmSetAnnotation,
		"lifecycle.opi.nvidia.com/release-namespace",
	} {
		obj := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
		obj.SetAnnotations(map[string]string{key: "https://evil.example"})
		if err := RejectInstallOverrides(obj); err == nil {
			t.Errorf("%s was accepted", key)
		}
	}
	ok := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	ok.SetAnnotations(map[string]string{VersionAnnotation: "v26.4.0"})
	if err := RejectInstallOverrides(ok); err != nil {
		t.Fatal(err)
	}
	urlPin := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	urlPin.SetAnnotations(map[string]string{VersionAnnotation: "https://evil.example/chart.tgz"})
	if err := RejectInstallOverrides(urlPin); err == nil {
		t.Fatal("version pin URL was accepted")
	}
}

func TestApplyVersionPolicyUsesPinnedChart(t *testing.T) {
	settings, _ := DefaultInstallConfig(VendorNVIDIA)
	in := Intent{Vendor: VendorNVIDIA, Known: true}
	got, pol := ApplyVersionPolicy(in, settings)
	if !reflect.DeepEqual(got, settings) {
		t.Fatalf("settings mutated: %+v", got)
	}
	if got.ChartVersion != NVIDIAChartVersion {
		t.Fatalf("empty pin must still pin the chart: %+v", got)
	}
	if pol.Skew != SkewUnknown {
		t.Fatalf("empty installed version classifies Unknown, not a fake Supported: %+v", pol)
	}
	in.PinnedVersion = "v26.4.0"
	_, pol = ApplyVersionPolicy(in, settings)
	if pol.Skew != SkewSupported {
		t.Fatalf("in-window pin: %+v", pol)
	}
	in.PinnedVersion = "https://evil.example"
	_, pol = ApplyVersionPolicy(in, settings)
	if pol.Skew != SkewInvalidConfig {
		t.Fatalf("URL pin: %+v", pol)
	}
	in.PinnedVersion = "v25.4.0"
	_, pol = ApplyVersionPolicy(in, settings)
	if pol.Skew != SkewTooOld {
		t.Fatalf("too old: %+v", pol)
	}
	if settings.ChartVersion != NVIDIAChartVersion {
		t.Fatal("chart version must stay the pin even when the cluster is TooOld")
	}
}

func TestInstallConfigMatchesVersionsYAML(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		DPF struct {
			Release string `json:"release"`
			Helm    struct {
				Chart     string `json:"chart"`
				Repo      string `json:"repo"`
				Version   string `json:"version"`
				Release   string `json:"release"`
				Namespace string `json:"namespace"`
				Owner     string `json:"owner"`
			} `json:"helm"`
		} `json:"dpf"`
	}
	if err := yaml.Unmarshal(b, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.DPF.Helm.Chart != NVIDIAChartName || pin.DPF.Helm.Repo != NVIDIAChartRepo ||
		pin.DPF.Helm.Version != NVIDIAChartVersion || pin.DPF.Helm.Release != NVIDIAReleaseName ||
		pin.DPF.Helm.Namespace != DefaultNamespace || pin.DPF.Helm.Owner != ReleaseOwner {
		t.Fatalf("config/versions.yaml helm pin drifted from DefaultInstallConfig: %+v", pin.DPF.Helm)
	}
	if pin.DPF.Release != NVIDIAChartVersion {
		t.Fatalf("helm.version %s != dpf.release %s", pin.DPF.Helm.Version, pin.DPF.Release)
	}
}

func TestEnsureInstalledRejectsHelmOverride(t *testing.T) {
	obj := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	obj.SetAnnotations(map[string]string{
		helmArgsAnnotation: "--set image=evil",
	})
	rec := &recordingInstaller{}
	r := &LifecycleManager{Installer: rec}
	out, err := r.EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Policy.Skew != SkewInvalidConfig {
		t.Fatalf("skew = %s, want InvalidConfig", out.Policy.Skew)
	}
	if len(rec.calls) != 0 {
		t.Fatal("installer ran for a helm-args override")
	}
	if out.Settings.ChartRepo != NVIDIAChartRepo && out.Settings.ChartRepo != "" {
		t.Fatalf("must not copy the user URL into settings: %+v", out.Settings)
	}
}

func TestEnsureInstalledSettingsArePinned(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, "v26.4.0")
	rec := &recordingInstaller{result: InstallResult{Action: InstallDetected, Present: true, Version: "v26.4.0"}}
	out, err := (&LifecycleManager{Installer: rec, Probe: ReadyClusterProbe()}).
		EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstallConfig(out.Settings); err != nil {
		t.Fatal(err)
	}
	if out.Settings.ChartVersion != NVIDIAChartVersion || out.Settings.ReleaseOwner != ReleaseOwner {
		t.Fatalf("settings = %+v", out.Settings)
	}
}

func TestRejectOverridesOnNil(t *testing.T) {
	if err := RejectInstallOverrides((*unstructured.Unstructured)(nil)); err != nil {
		t.Fatal(err)
	}
}
