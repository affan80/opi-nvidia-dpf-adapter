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
	"sync"
	"testing"
	"time"
)

func ownedOlderRelease() HelmRelease {
	return HelmRelease{
		Name:         NVIDIAReleaseName,
		Namespace:    DefaultNamespace,
		ChartName:    NVIDIAChartName,
		ChartVersion: "v26.3.9",
		Status:       HelmStatusDeployed,
		Revision:     1,
		Labels:       map[string]string{ReleaseOwnerLabel: ReleaseOwner},
	}
}

func installerFor(mem *MemoryHelmClient) HelmInstaller {
	return HelmInstaller{Client: mem, Locks: newReleaseLocks()}
}

func TestSupportedUpgradeSucceeds(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
	}}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUpgraded || got.OpStatus != OpSucceeded || got.Version != NVIDIAChartVersion {
		t.Fatalf("upgrade = %+v", got)
	}
	if got.LastKnownGood != "v26.3.9" {
		t.Fatalf("last-known-good = %s", got.LastKnownGood)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.Identity() != "dpf-operator-system/dpf-operator@v26.4.0" || mem.count() != 1 {
		t.Fatalf("after upgrade %+v count=%d", rel, mem.count())
	}
}

func TestSameVersionIsNoop(t *testing.T) {
	cfg := mustNVIDIAConfig(t)
	mem := &MemoryHelmClient{}
	if _, err := mem.Install(context.Background(), cfg, SafeHelmValues(cfg)); err != nil {
		t.Fatal(err)
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallNoop || got.OpStatus != OpNoop || mem.Upgrades != 0 {
		t.Fatalf("noop = %+v upgrades=%d", got, mem.Upgrades)
	}
}

func TestUnsupportedAndMalformedVersionsRejected(t *testing.T) {
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): {
			Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
			ChartName: NVIDIAChartName, ChartVersion: "latest",
			Status: HelmStatusDeployed,
			Labels: map[string]string{ReleaseOwnerLabel: ReleaseOwner},
		},
	}}
	_, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrMalformedVersion) {
		t.Fatalf("malformed: %v", err)
	}
	mem.rels[releaseKey(DefaultNamespace, NVIDIAReleaseName)] = HelmRelease{
		Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
		ChartName: NVIDIAChartName, ChartVersion: "v27.1.0",
		Status: HelmStatusDeployed,
		Labels: map[string]string{ReleaseOwnerLabel: ReleaseOwner},
	}
	_, err = installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrDowngradeForbidden) {
		t.Fatalf("too-new vs pin: %v", err)
	}
}

func TestDowngradeRejectedUnlessAllowed(t *testing.T) {
	rel := ownedOlderRelease()
	rel.ChartVersion = "v26.4.2"
	mem := &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(DefaultNamespace, NVIDIAReleaseName): rel,
	}}
	_, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrDowngradeForbidden) {
		t.Fatalf("downgrade: %v", err)
	}
	if mem.Upgrades != 0 {
		t.Fatal("rejected downgrade must not call Upgrade")
	}
	got, err := HelmInstaller{Client: mem, Locks: newReleaseLocks(), AllowDowngrade: true}.
		Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUpgraded || got.Version != NVIDIAChartVersion {
		t.Fatalf("allowed downgrade = %+v", got)
	}
}

func TestConcurrentOperationsAreSerialized(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		Hold: 40 * time.Millisecond,
	}
	h := installerFor(mem)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := h.Ensure(context.Background(), nvidiaIntent())
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if mem.MaxInflight > 1 {
		t.Fatalf("max inflight = %d, want serialized 1", mem.MaxInflight)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d", mem.count())
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if !versionsEqual(rel.ChartVersion, NVIDIAChartVersion) {
		t.Fatalf("after concurrent ensure version = %s", rel.ChartVersion)
	}
}

func TestUpgradeFailurePreservesLastKnownGood(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		UpgradeErr:    errors.New("chart pull failed"),
		FailNUpgrades: 99,
		RollbackErr:   errors.New("rollback denied"),
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("err = %v, want rollback failed", err)
	}
	if got.LastKnownGood != "v26.3.9" {
		t.Fatalf("last-known-good lost: %+v", got)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.ChartVersion != "v26.3.9" {
		t.Fatalf("current rewritten to %s", rel.ChartVersion)
	}
	saved, ok := mem.LastKnownGood(DefaultNamespace, NVIDIAReleaseName)
	if !ok || saved.ChartVersion != "v26.3.9" {
		t.Fatalf("stored last-known-good = %+v", saved)
	}
}

func TestTimeoutReconcilesActualState(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		Timeout: true,
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrReleaseTimeout) {
		t.Fatalf("timeout err = %v", err)
	}
	if got.OpStatus != OpTimeout || got.Action == InstallUpgraded {
		t.Fatalf("timeout reported success: %+v", got)
	}
	if got.Observed != "v26.3.9" || got.LastKnownGood != "v26.3.9" {
		t.Fatalf("timeout did not reconcile actual: %+v", got)
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if rel.ChartVersion != "v26.3.9" {
		t.Fatalf("timeout mutated release to %s", rel.ChartVersion)
	}
}

func TestUnknownOutcomeIsNotRetried(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		Unknown: true,
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrReleaseUnknown) {
		t.Fatalf("unknown err = %v", err)
	}
	if mem.Upgrades != 1 {
		t.Fatalf("upgrades = %d, unknown outcome must not be retried", mem.Upgrades)
	}
	if got.OpStatus != OpUnknown || got.Action == InstallUpgraded {
		t.Fatalf("unknown reported success: %+v", got)
	}
}

func TestRetrySucceedsWithoutDuplicateRelease(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		UpgradeErr:    errors.New("transient"),
		FailNUpgrades: 1,
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUpgraded || got.OpStatus != OpSucceeded {
		t.Fatalf("retry = %+v", got)
	}
	if mem.Upgrades != 2 {
		t.Fatalf("upgrades = %d, want 2 (one fail, one retry)", mem.Upgrades)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d", mem.count())
	}
	rel, _ := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if !versionsEqual(rel.ChartVersion, NVIDIAChartVersion) {
		t.Fatalf("after retry version = %s", rel.ChartVersion)
	}
}

func TestSuccessfulRollbackRestoresExpectedRelease(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		UpgradeErr:    errors.New("render failed"),
		FailNUpgrades: 99,
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallRolledBack || got.OpStatus != OpRolledBack {
		t.Fatalf("rollback = %+v", got)
	}
	if got.Observed != "v26.3.9" || got.Version != "v26.3.9" {
		t.Fatalf("rollback did not restore v26.3.9: %+v", got)
	}
	if mem.Rollbacks != 1 {
		t.Fatalf("rollbacks = %d", mem.Rollbacks)
	}
}

func TestFailedRollbackIsNotSuccess(t *testing.T) {
	mem := &MemoryHelmClient{
		rels: map[string]HelmRelease{
			releaseKey(DefaultNamespace, NVIDIAReleaseName): ownedOlderRelease(),
		},
		UpgradeErr:    errors.New("render failed"),
		FailNUpgrades: 99,
		RollbackErr:   errors.New("history missing"),
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("err = %v", err)
	}
	if got.OpStatus != OpRollbackFailed || got.Action == InstallUpgraded || got.Action == InstallRolledBack {
		t.Fatalf("failed rollback looks successful: %+v", got)
	}
	if got.LastKnownGood != "v26.3.9" {
		t.Fatalf("last-known-good dropped: %+v", got)
	}
}

func TestHelmClientDoesNotLogSecretsOrShellOut(t *testing.T) {
	b, err := os.ReadFile("helm_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{
		"exec.Command", "os/exec", "helm --set", "Bearer ",
	} {
		if strings.Contains(src, banned) {
			t.Errorf("helm_client.go contains %q", banned)
		}
	}
	if !strings.Contains(src, "SanitizeLogValue") {
		t.Error("helm_client.go must keep SanitizeLogValue for sensitive values")
	}
}
