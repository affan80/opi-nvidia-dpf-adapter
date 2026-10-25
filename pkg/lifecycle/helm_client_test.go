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
	"strings"
	"testing"
)

func nvidiaIntent() Intent {
	return Intent{Vendor: VendorNVIDIA, Known: true, PinnedVersion: NVIDIAChartVersion}
}

func TestHelmFreshInstallConvergesOnPinnedRelease(t *testing.T) {
	mem := &MemoryHelmClient{}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallInstalled || !got.Present || got.Version != NVIDIAChartVersion {
		t.Fatalf("fresh install = %+v, want installed/%s", got, NVIDIAChartVersion)
	}
	rel, err := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	want := OwnedRelease(mustNVIDIAConfig(t))
	if rel.Identity() != want.Identity() || !rel.ManagedByAdapter() {
		t.Fatalf("release = %+v, want %s owned by %s", rel, want.Identity(), ReleaseOwner)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d, want 1", mem.count())
	}
}

func TestHelmAlreadyInstalledConvergesOnSameRelease(t *testing.T) {
	cfg := mustNVIDIAConfig(t)
	mem := &MemoryHelmClient{}
	if _, err := mem.Install(context.Background(), cfg, SafeHelmValues(cfg)); err != nil {
		t.Fatal(err)
	}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallNoop || !got.Present || got.Version != NVIDIAChartVersion {
		t.Fatalf("same version = %+v, want noop/%s", got, NVIDIAChartVersion)
	}
	rel, err := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Identity() != OwnedRelease(cfg).Identity() {
		t.Fatalf("adopted identity = %s, want %s", rel.Identity(), OwnedRelease(cfg).Identity())
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d, want 1 (adopt must not create a second release)", mem.count())
	}
}

func TestHelmFreshAndAdoptedShareOneReleaseIdentity(t *testing.T) {
	fresh := &MemoryHelmClient{}
	if _, err := (HelmInstaller{Client: fresh}).Ensure(context.Background(), nvidiaIntent()); err != nil {
		t.Fatal(err)
	}
	owned := &MemoryHelmClient{}
	cfg := mustNVIDIAConfig(t)
	if _, err := owned.Install(context.Background(), cfg, SafeHelmValues(cfg)); err != nil {
		t.Fatal(err)
	}
	if _, err := (HelmInstaller{Client: owned}).Ensure(context.Background(), nvidiaIntent()); err != nil {
		t.Fatal(err)
	}

	a, err := fresh.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	b, err := owned.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if a.Identity() != b.Identity() {
		t.Fatalf("fresh %s != adopted %s", a.Identity(), b.Identity())
	}
	if a.Identity() != "dpf-operator-system/dpf-operator@v26.4.0" {
		t.Fatalf("identity = %s, want the pinned NVIDIA release", a.Identity())
	}
	if !a.ManagedByAdapter() || !b.ManagedByAdapter() {
		t.Fatal("both paths must stamp the adapter owner")
	}
}

func TestHelmAdoptsOwnedSkewedReleaseOntoPin(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): {
			Name:         NVIDIAReleaseName,
			Namespace:    DefaultNamespace,
			ChartName:    NVIDIAChartName,
			ChartVersion: "v26.3.9",
			Status:       HelmStatusDeployed,
			Labels:       map[string]string{ReleaseOwnerLabel: ReleaseOwner},
		},
	}}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUpgraded || got.Version != NVIDIAChartVersion {
		t.Fatalf("skew upgrade = %+v, want upgraded/%s", got, NVIDIAChartVersion)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.Identity() != OwnedRelease(mustNVIDIAConfig(t)).Identity() {
		t.Fatalf("after upgrade identity = %s", rel.Identity())
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d, want 1", mem.count())
	}
}

func TestHelmDoesNotAdoptUnmanagedRelease(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): {
			Name:         NVIDIAReleaseName,
			Namespace:    DefaultNamespace,
			ChartName:    NVIDIAChartName,
			ChartVersion: NVIDIAChartVersion,
			Status:       HelmStatusDeployed,
			Labels:       map[string]string{"owner": "helm"},
		},
	}}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUnmanaged || !got.Present {
		t.Fatalf("unmanaged = %+v, want unmanaged/present", got)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.ManagedByAdapter() {
		t.Fatal("must not stamp owner onto an unmanaged release")
	}
	if rel.Labels["owner"] != "helm" {
		t.Fatal("unmanaged labels must stay untouched")
	}
}

func labUnmanagedRelease() HelmRelease {
	return HelmRelease{
		Name:         NVIDIAReleaseName,
		Namespace:    DefaultNamespace,
		ChartName:    NVIDIAChartName,
		ChartVersion: NVIDIAChartVersion,
		Status:       HelmStatusDeployed,
		Labels:       map[string]string{helmStorageOwnerLabel: "helm"},
	}
}

func TestHelmClaimsMatchingPinWhenAdoptExisting(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): labUnmanagedRelease(),
	}}
	in := nvidiaIntent()
	in.AdoptExisting = true
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallAdopted {
		t.Fatalf("claim = %+v, want adopted", got)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.Identity() != "dpf-operator-system/dpf-operator@v26.4.0" {
		t.Fatalf("identity = %s, claim must not create a second release", rel.Identity())
	}
	if !rel.ManagedByAdapter() || rel.Labels[ReleaseModeLabel] != ReleaseModeAdopted {
		t.Fatalf("labels = %+v, want adapter owner and mode=adopted", rel.Labels)
	}
	if rel.Labels[helmStorageOwnerLabel] != "helm" {
		t.Fatal("Helm storage owner=helm must stay; do not rewrite the release")
	}
	if mem.count() != 1 || mem.Uninstalls != 0 || mem.Upgrades != 0 {
		t.Fatalf("claim mutated Helm history: count=%d uninstalls=%d upgrades=%d",
			mem.count(), mem.Uninstalls, mem.Upgrades)
	}
	again, err := HelmInstaller{Client: mem}.Ensure(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if again.Action != InstallNoop {
		t.Fatalf("second ensure = %+v, want noop on the claimed pin", again)
	}
}

func TestHelmDoesNotClaimWrongVersionEvenWithAdoptExisting(t *testing.T) {
	rel := labUnmanagedRelease()
	rel.ChartVersion = "v26.3.9"
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): rel,
	}}
	in := nvidiaIntent()
	in.AdoptExisting = true
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUnmanaged {
		t.Fatalf("older chart = %+v, want unmanaged (do not claim then upgrade)", got)
	}
	kept, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if kept.ManagedByAdapter() {
		t.Fatal("must not claim a release that is not the pin")
	}
}

func TestHelmUpgradeRefusesUnmanaged(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): {
			Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
			Labels: map[string]string{},
		},
	}}
	_, err := mem.Upgrade(context.Background(), mustNVIDIAConfig(t), nil)
	if !errors.Is(err, ErrReleaseUnmanaged) {
		t.Fatalf("upgrade unmanaged: %v, want ErrReleaseUnmanaged", err)
	}
}

func TestSafeHelmValuesPinnedAndArgFree(t *testing.T) {
	cfg := mustNVIDIAConfig(t)
	vals := SafeHelmValues(cfg)
	if vals == nil {
		t.Fatal("safe values must not be nil for nvidia")
	}
	kamaji, _ := vals["kamajiClusterManager"].(map[string]any)
	if kamaji["disable"] != true {
		t.Fatalf("kamaji disable = %v, want true", kamaji["disable"])
	}
	if len(cfg.HelmArgs) != 0 {
		t.Fatal("HelmArgs must stay empty")
	}
	evil := cfg
	evil.HelmArgs = []string{"--set", "image=evil"}
	if SafeHelmValues(evil) != nil {
		t.Fatal("values with helm args must be rejected")
	}
}

func TestHelmInstallerAMDSkipsChart(t *testing.T) {
	mem := &MemoryHelmClient{}
	got, err := HelmInstaller{Client: mem}.Ensure(context.Background(), Intent{Vendor: VendorAMD, Known: true})
	if !errors.Is(err, ErrNoVendorChart) {
		t.Fatalf("err = %v, want ErrNoVendorChart", err)
	}
	if got.Action != InstallSkipped || mem.count() != 0 {
		t.Fatalf("AMD = %+v releases=%d, want skipped and no chart", got, mem.count())
	}
}

func TestEnsureInstalledHelmFreshAndAdopted(t *testing.T) {
	mem := &MemoryHelmClient{}
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	fresh, err := (&LifecycleManager{Helm: mem, Probe: ReadyClusterProbe()}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Install.Action != InstallInstalled {
		t.Fatalf("fresh EnsureInstalled = %+v, want installed", fresh.Install)
	}
	again, err := (&LifecycleManager{Helm: mem, Probe: ReadyClusterProbe()}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if again.Install.Action != InstallNoop {
		t.Fatalf("second EnsureInstalled = %+v, want noop", again.Install)
	}
	if fresh.Settings.ReleaseName != again.Settings.ReleaseName ||
		fresh.Settings.Namespace != again.Settings.Namespace ||
		fresh.Settings.ChartVersion != again.Settings.ChartVersion {
		t.Fatalf("settings diverged: %+v vs %+v", fresh.Settings, again.Settings)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d, want 1", mem.count())
	}
}

func TestHelmClientSourceHasNoCLI(t *testing.T) {
	b, err := os.ReadFile("helm_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"helm.New", "exec.Command(\"helm\"", "os/exec"} {
		if strings.Contains(src, banned) {
			t.Errorf("helm_client.go contains %q; use the Helm SDK interface, not the CLI", banned)
		}
	}
}

func mustNVIDIAConfig(t *testing.T) InstallConfig {
	t.Helper()
	cfg, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstallConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
