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

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func TestVersionsYAMLPinsAMDNoChart(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		AMD struct {
			Helm struct {
				Published bool   `json:"published"`
				Chart     string `json:"chart"`
				Repo      string `json:"repo"`
				Version   string `json:"version"`
				Release   string `json:"release"`
			} `json:"helm"`
			OLM struct {
				Published bool   `json:"published"`
				Package   string `json:"package"`
			} `json:"olm"`
		} `json:"amd"`
	}
	if err := yaml.Unmarshal(b, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.AMD.Helm.Published || pin.AMD.Helm.Chart != "" || pin.AMD.Helm.Repo != "" ||
		pin.AMD.Helm.Version != "" || pin.AMD.Helm.Release != "" {
		t.Fatalf("amd helm must stay unpublished: %+v", pin.AMD.Helm)
	}
	if pin.AMD.OLM.Published || pin.AMD.OLM.Package != "" {
		t.Fatalf("amd olm must stay unpublished: %+v", pin.AMD.OLM)
	}
	body := string(b)
	for _, banned := range []string{
		"network-operator",
		"pensando/lago",
		"instinct.docs.amd.com",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("versions.yaml must not pin %q as the AMD DSC operator", banned)
		}
	}
}

func TestEnsureInstalledAMDWithHelmReportsAbsentNotSkipped(t *testing.T) {
	mem := &MemoryHelmClient{}
	obj := newConfig("cfg", map[string]any{"vendor": VendorAMD})
	out, err := (&LifecycleManager{
		Helm: mem, Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallAbsent || out.Install.Present {
		t.Fatalf("BlueField AMD path = %+v, want absent (Helm skip must not hide detect)", out.Install)
	}
	if mem.count() != 0 {
		t.Fatal("AMD must not write a Helm release")
	}
	if out.Policy.Skew != SkewUnpinned {
		t.Fatalf("skew = %s, want Unpinned", out.Policy.Skew)
	}
	if err := ValidateInstallConfig(out.Settings); err != nil {
		t.Fatal(err)
	}
	if out.Settings.ChartName != "" || out.Settings.ChartRepo != "" {
		t.Fatalf("settings must not invent an AMD chart: %+v", out.Settings)
	}
}

func TestHelmWithDetectAMDFallsThroughToDSCDetect(t *testing.T) {
	mem := &MemoryHelmClient{}
	got, err := (helmWithDetect{
		Helm: HelmInstaller{Client: mem},
		Next: DetectingInstaller{Mapper: stubMapper{present: amdProbeGVK.GroupKind()}},
	}).Ensure(context.Background(), Intent{Vendor: VendorAMD, Known: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Action != InstallDetected {
		t.Fatalf("DSC present = %+v, want detected; Helm must not swallow AMD", got)
	}
	if mem.count() != 0 {
		t.Fatal("detecting a DSC API must not install a chart")
	}
}

func TestHelmInstallerAMDReturnsNoVendorChart(t *testing.T) {
	mem := &MemoryHelmClient{}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), Intent{Vendor: VendorAMD, Known: true})
	if !errors.Is(err, ErrNoVendorChart) {
		t.Fatalf("err = %v, want ErrNoVendorChart so the detect chain can run", err)
	}
	if got.Action != InstallSkipped || mem.count() != 0 {
		t.Fatalf("AMD Helm = %+v releases=%d, want skipped and no chart", got, mem.count())
	}
}

func TestOLMInstallerAMDReturnsNoVendorChart(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{Vendor: VendorAMD, Known: true})
	if !errors.Is(err, ErrNoVendorChart) {
		t.Fatalf("err = %v, want ErrNoVendorChart", err)
	}
	if got.Action != InstallSkipped || mem.SubscribeCalls != 0 {
		t.Fatalf("AMD OLM = %+v subscribe=%d", got, mem.SubscribeCalls)
	}
}

func TestEnsureInstalledAMDDoesNotAdoptNVIDIARelease(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})
	obj := newConfig("cfg", map[string]any{"vendor": VendorAMD})
	obj.SetAnnotations(map[string]string{AdoptExistingAnnotation: "true"})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec, obj).Build()
	out, err := (&LifecycleManager{
		Client: cl, Helm: NewSecretHelmClient(cl), Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action == InstallAdopted || out.Install.Action == InstallInstalled {
		t.Fatalf("AMD must not claim the NVIDIA Helm pin: %+v", out.Install)
	}
	if out.Install.Action != InstallAbsent {
		t.Fatalf("AMD on a BlueField-shaped client = %+v, want absent", out.Install)
	}
}
