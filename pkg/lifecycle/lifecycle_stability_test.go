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
	"sync"
	"testing"
	"time"
)

func TestHelmAbsentReleaseInstallsPinned(t *testing.T) {
	mem := &MemoryHelmClient{}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallInstalled || got.OpStatus != OpSucceeded {
		t.Fatalf("absent install = %+v", got)
	}
	rel, err := mem.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil || rel.Identity() != "dpf-operator-system/dpf-operator@v26.4.0" {
		t.Fatalf("release = %+v err=%v", rel, err)
	}
}

func TestHelmInstallFailureLeavesNoRelease(t *testing.T) {
	mem := &MemoryHelmClient{InstallErr: errors.New("chart pull failed"), FailNInstalls: 99}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err == nil {
		t.Fatalf("want install error, got %+v", got)
	}
	if mem.count() != 0 {
		t.Fatalf("failed install left %d releases", mem.count())
	}
	if got.Action != InstallFailed {
		t.Fatalf("action = %s", got.Action)
	}
}

func TestHelmInstallRecoversOnRetry(t *testing.T) {
	mem := &MemoryHelmClient{InstallErr: errors.New("transient"), FailNInstalls: 1}
	if _, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent()); err == nil {
		t.Fatal("first ensure should fail")
	}
	got, err := installerFor(mem).Ensure(context.Background(), nvidiaIntent())
	if err != nil || got.Action != InstallInstalled {
		t.Fatalf("recovery = %+v err=%v", got, err)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d", mem.count())
	}
}

func TestEnsureHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mem := &MemoryHelmClient{}
	_, err := installerFor(mem).Ensure(ctx, nvidiaIntent())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
	if mem.count() != 0 {
		t.Fatal("canceled ensure must not install")
	}
}

func TestEnsureHonorsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	mem := memWith(ownedOlderRelease())
	got, err := installerFor(mem).Ensure(ctx, nvidiaIntent())
	if err == nil || got.OpStatus == OpSucceeded {
		t.Fatalf("deadline reported success: %+v err=%v", got, err)
	}
}

func TestRepeatedReconcileIsIdempotentNoExtraUpgrade(t *testing.T) {
	cfg := mustNVIDIAConfig(t)
	mem := &MemoryHelmClient{}
	if _, err := mem.Install(context.Background(), cfg, SafeHelmValues(cfg)); err != nil {
		t.Fatal(err)
	}
	h := installerFor(mem)
	for range 5 {
		got, err := h.Ensure(context.Background(), nvidiaIntent())
		if err != nil || got.Action != InstallNoop || got.OpStatus != OpNoop {
			t.Fatalf("repeat = %+v err=%v", got, err)
		}
	}
	if mem.Upgrades != 0 {
		t.Fatalf("upgrades = %d, repeated ensure must be a no-op", mem.Upgrades)
	}
}

func TestRestartRecoversLastKnownGoodWithoutDuplicate(t *testing.T) {
	mem := memWith(ownedOlderRelease())
	first := installerFor(mem)
	got, err := first.Ensure(context.Background(), nvidiaIntent())
	if err != nil || got.Action != InstallUpgraded {
		t.Fatalf("first = %+v err=%v", got, err)
	}
	restarted := installerFor(mem)
	again, err := restarted.Ensure(context.Background(), nvidiaIntent())
	if err != nil || again.Action != InstallNoop {
		t.Fatalf("after restart = %+v err=%v", again, err)
	}
	if mem.count() != 1 {
		t.Fatalf("releases = %d", mem.count())
	}
	if got.LastKnownGood != "v26.3.9" {
		t.Fatalf("last-known-good lost across restart path: %+v", got)
	}
}

func TestConcurrentUpgradeAndCleanupAreSerialized(t *testing.T) {
	mem := memWith(ownedOlderRelease())
	locks := newReleaseLocks()
	h := HelmInstaller{Client: mem, Locks: locks}
	r := &LifecycleManager{Helm: mem, Index: &MemoryResourceIndex{}, Locks: locks}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() {
		_, err := h.Ensure(context.Background(), nvidiaIntent())
		errs <- err
	})
	wg.Go(func() {
		_ = r.CleanupInstallations(context.Background(), newConfig("cfg", map[string]any{"vendor": "nvidia"}))
		errs <- nil
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if mem.MaxInflight > 1 {
		t.Fatalf("max inflight = %d", mem.MaxInflight)
	}
	if mem.count() != 1 {
		t.Fatalf("release count = %d", mem.count())
	}
}

func TestInvalidRemovalPolicyDefaultsToRetain(t *testing.T) {
	mem := memWith(controllerRelease())
	cfg := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	cfg.SetAnnotations(map[string]string{RemovalPolicyAnnotation: "destroy-everything"})
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), cfg)
	if got.Action != CleanupPreserved || mem.count() != 1 {
		t.Fatalf("invalid policy = %+v count=%d", got, mem.count())
	}
}

func TestHelmUninstallFailureIsNotSuccess(t *testing.T) {
	mem := memWith(controllerRelease())
	mem.UninstallErr = errors.New("storage conflict")
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupFailed || got.OpStatus == OpSucceeded {
		t.Fatalf("uninstall failure = %+v", got)
	}
	if mem.count() != 1 {
		t.Fatal("failed uninstall must leave the release")
	}
}

func TestLockReleasedAfterEnsureSoRetryCanProceed(t *testing.T) {
	mem := memWith(ownedOlderRelease())
	h := installerFor(mem)
	if _, err := h.Ensure(context.Background(), nvidiaIntent()); err != nil {
		t.Fatal(err)
	}
	got, err := h.Ensure(context.Background(), nvidiaIntent())
	if err != nil || got.Action != InstallNoop {
		t.Fatalf("lock held? %+v err=%v", got, err)
	}
}
