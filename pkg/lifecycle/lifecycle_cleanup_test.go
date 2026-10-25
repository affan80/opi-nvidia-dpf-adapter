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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func deleteOwnedConfig() *unstructured.Unstructured {
	c := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	c.SetAnnotations(map[string]string{RemovalPolicyAnnotation: RemovalDeleteOwned})
	return c
}

func adoptDeleteConfig() *unstructured.Unstructured {
	c := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	c.SetAnnotations(map[string]string{
		RemovalPolicyAnnotation:  RemovalRetain,
		AdoptionPolicyAnnotation: RemovalDeleteOwned,
	})
	return c
}

func controllerRelease() HelmRelease {
	return HelmRelease{
		Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
		ChartName: NVIDIAChartName, ChartVersion: NVIDIAChartVersion,
		Status: HelmStatusDeployed,
		Labels: map[string]string{
			ReleaseOwnerLabel: ReleaseOwner,
			ReleaseModeLabel:  ReleaseModeInstalled,
		},
	}
}

func adoptedRel() HelmRelease {
	rel := controllerRelease()
	rel.Labels[ReleaseModeLabel] = ReleaseModeAdopted
	return rel
}

func externalRel() HelmRelease {
	return HelmRelease{
		Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
		ChartName: NVIDIAChartName, ChartVersion: NVIDIAChartVersion,
		Status: HelmStatusDeployed,
		Labels: map[string]string{helmStorageOwnerLabel: "helm"},
	}
}

func memWith(rel HelmRelease) *MemoryHelmClient {
	return &MemoryHelmClient{rels: map[string]HelmRelease{
		releaseKey(rel.Namespace, rel.Name): rel,
	}}
}

func cleanupMgr(helm HelmClient, idx ResourceIndex) *LifecycleManager {
	return &LifecycleManager{Helm: helm, Index: idx, Locks: newReleaseLocks()}
}

func TestControllerOwnedReleaseDeletedWhenPolicyPermits(t *testing.T) {
	mem := memWith(controllerRelease())
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupDeleted || got.Ownership != OwnershipController {
		t.Fatalf("got %+v", got)
	}
	if mem.count() != 0 || mem.Uninstalls != 1 {
		t.Fatalf("releases=%d uninstalls=%d", mem.count(), mem.Uninstalls)
	}
}

func TestAdoptedReleaseFollowsAdoptionPolicy(t *testing.T) {
	mem := memWith(adoptedRel())
	retain := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(
		context.Background(), newConfig("cfg", map[string]any{"vendor": "nvidia"}))
	if retain.Action != CleanupPreserved || mem.count() != 1 {
		t.Fatalf("default adopt retain = %+v count=%d", retain, mem.count())
	}
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), adoptDeleteConfig())
	if got.Action != CleanupDeleted || got.Ownership != OwnershipAdopted {
		t.Fatalf("adopt delete-owned = %+v", got)
	}
	if mem.count() != 0 {
		t.Fatal("adopted release should be gone when adoption policy allows")
	}
}

func TestExternallyOwnedReleaseIsPreserved(t *testing.T) {
	mem := memWith(externalRel())
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupPreserved || got.Ownership != OwnershipExternal {
		t.Fatalf("external = %+v", got)
	}
	if mem.count() != 1 || mem.Uninstalls != 0 {
		t.Fatal("external release must not be uninstalled")
	}
}

func TestMissingOrConflictingOwnershipFailsSafe(t *testing.T) {
	missing := HelmRelease{
		Name: NVIDIAReleaseName, Namespace: DefaultNamespace,
		ChartName: NVIDIAChartName, ChartVersion: NVIDIAChartVersion,
		Status: HelmStatusDeployed,
	}
	mem := memWith(missing)
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupFailed || mem.count() != 1 {
		t.Fatalf("missing labels = %+v count=%d", got, mem.count())
	}
	if !strings.Contains(got.Reason, "missing") && !strings.Contains(strings.Join(got.Skipped, " "), "missing") {
		t.Fatalf("missing ownership not explained: %+v", got)
	}

	conflict := controllerRelease()
	conflict.Labels[ReleaseOwnerLabel] = "other-operator"
	conflict.Labels[helmStorageOwnerLabel] = "helm"
	mem = memWith(conflict)
	got = cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupPreserved || got.Ownership != OwnershipExternal || mem.Uninstalls != 0 {
		t.Fatalf("other owner = %+v uninstalls=%d", got, mem.Uninstalls)
	}

	claimed := labUnmanagedRelease()
	claimed.Labels = stampAdoptedLabels(claimed.Labels)
	mem = memWith(claimed)
	got = cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), newConfig("cfg", map[string]any{"vendor": "nvidia"}))
	if got.Action != CleanupPreserved || got.Ownership != OwnershipAdopted || mem.Uninstalls != 0 {
		t.Fatalf("claimed pin default retain = %+v uninstalls=%d", got, mem.Uninstalls)
	}
}

func TestSharedCRDsPreservedWhileOthersDepend(t *testing.T) {
	mem := memWith(controllerRelease())
	idx := &MemoryResourceIndex{Tenants: []string{"cfg", "other-tenant"}, CRDDependents: 2}
	r := &LifecycleManager{Helm: mem, Index: idx, Locks: newReleaseLocks(), AllowCRDDeletion: true}
	got := r.CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.DeletedCRDs {
		t.Fatalf("shared CRDs deleted: %+v", got)
	}
	if !containsSkip(got.Skipped, "crds") {
		t.Fatalf("CRD skip not observable: %+v", got)
	}
}

func TestSharedDataplanePreservedWhileReferenced(t *testing.T) {
	mem := memWith(controllerRelease())
	dpu := ObjectRef{
		Kind: dpuKind, Namespace: DefaultNamespace, Name: "bluefield",
		Owner: ReleaseOwner, Referenced: true,
	}
	idx := &MemoryResourceIndex{
		OPI:       []ObjectRef{{Kind: "DataProcessingUnit", Namespace: DefaultNamespace, Name: "bluefield"}},
		Dataplane: []ObjectRef{dpu},
	}
	got := cleanupMgr(mem, idx).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if len(idx.Deleted) != 0 {
		t.Fatalf("referenced dataplane deleted: %+v", idx.Deleted)
	}
	if !containsSkip(got.Skipped, "still referenced") {
		t.Fatalf("dataplane skip not observable: %+v", got)
	}
}

func TestNamespaceScopedCleanupDoesNotDeletePeers(t *testing.T) {
	mem := memWith(controllerRelease())
	ours := ObjectRef{Kind: dpuKind, Namespace: DefaultNamespace, Name: "bluefield", Owner: ReleaseOwner}
	peer := ObjectRef{Kind: dpuKind, Namespace: "other-tenant", Name: "bluefield", Owner: ReleaseOwner}
	idx := &MemoryResourceIndex{Dataplane: []ObjectRef{ours, peer}}
	got := cleanupMgr(mem, idx).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupDeleted {
		t.Fatalf("action = %+v", got)
	}
	if len(idx.Deleted) != 1 || idx.Deleted[0].Namespace != DefaultNamespace {
		t.Fatalf("deleted = %+v", idx.Deleted)
	}
	if !containsSkip(got.Skipped, "different namespace") {
		t.Fatalf("peer skip not observable: %+v", got)
	}
	for _, d := range idx.Dataplane {
		if d.Namespace == "other-tenant" && d.Name == "bluefield" {
			return
		}
	}
	t.Fatal("same-name object in another namespace was deleted")
}

type flipHelm struct {
	inner *MemoryHelmClient
	gets  int
}

func (f *flipHelm) GetRelease(ctx context.Context, ns, name string) (HelmRelease, error) {
	rel, err := f.inner.GetRelease(ctx, ns, name)
	f.gets++
	if f.gets >= 2 && err == nil {
		rel.Labels = map[string]string{helmStorageOwnerLabel: "helm"}
	}
	return rel, err
}
func (f *flipHelm) Install(ctx context.Context, cfg InstallConfig, v map[string]any) (HelmRelease, error) {
	return f.inner.Install(ctx, cfg, v)
}
func (f *flipHelm) Upgrade(ctx context.Context, cfg InstallConfig, v map[string]any) (HelmRelease, error) {
	return f.inner.Upgrade(ctx, cfg, v)
}
func (f *flipHelm) Rollback(ctx context.Context, cfg InstallConfig, to string) (HelmRelease, error) {
	return f.inner.Rollback(ctx, cfg, to)
}
func (f *flipHelm) Uninstall(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	return f.inner.Uninstall(ctx, cfg)
}

func TestOwnershipChangeBeforeDeleteIsPreserved(t *testing.T) {
	inner := memWith(controllerRelease())
	flip := &flipHelm{inner: inner}
	got := cleanupMgr(flip, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupFailed || got.Failure != ErrCleanupChanged.Error() {
		t.Fatalf("race = %+v, want ownership changed", got)
	}
	if inner.count() != 1 || inner.Uninstalls != 0 {
		t.Fatalf("race deleted the release: count=%d uninstalls=%d", inner.count(), inner.Uninstalls)
	}
}

func TestCleanupIsIdempotentAndDoesNotBypassOwnership(t *testing.T) {
	mem := memWith(controllerRelease())
	r := cleanupMgr(mem, &MemoryResourceIndex{})
	first := r.CleanupInstallations(context.Background(), deleteOwnedConfig())
	if first.Action != CleanupDeleted {
		t.Fatalf("first = %+v", first)
	}
	second := r.CleanupInstallations(context.Background(), deleteOwnedConfig())
	if second.Action != CleanupGone {
		t.Fatalf("second = %+v", second)
	}

	ext := memWith(externalRel())
	er := cleanupMgr(ext, &MemoryResourceIndex{})
	for range 3 {
		got := er.CleanupInstallations(context.Background(), deleteOwnedConfig())
		if got.Action != CleanupPreserved || ext.count() != 1 || ext.Uninstalls != 0 {
			t.Fatalf("retry bypassed ownership: %+v count=%d", got, ext.count())
		}
	}
}

func TestCleanupFailuresAndSkipsAreRecorded(t *testing.T) {
	mem := memWith(externalRel())
	got := cleanupMgr(mem, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if got.Action != CleanupPreserved || len(got.Skipped) == 0 {
		t.Fatalf("skip not recorded: %+v", got)
	}
	if got.Reason == "" {
		t.Fatal("skip reason is empty")
	}

	nilHelm := cleanupMgr(nil, &MemoryResourceIndex{}).CleanupInstallations(context.Background(), deleteOwnedConfig())
	if nilHelm.Action != CleanupFailed || nilHelm.OpStatus != OpFailed {
		t.Fatalf("nil helm reported success: %+v", nilHelm)
	}
}

func TestCleanupReconcilePreservesExternalAndDropsFinalizer(t *testing.T) {
	sch := lcmScheme()
	obj := deleteOwnedConfig()
	now := metav1.Now()
	obj.SetDeletionTimestamp(&now)
	obj.SetFinalizers([]string{CleanupFinalizer})
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(obj).WithStatusSubresource(obj).Build()
	mem := memWith(externalRel())
	r := &LifecycleManager{
		Client: cl, Scheme: sch, Helm: mem, Probe: ReadyClusterProbe(),
		Index: &MemoryResourceIndex{}, Locks: newReleaseLocks(),
	}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cfg"},
	}); err != nil {
		t.Fatal(err)
	}
	if mem.count() != 1 {
		t.Fatal("reconcile deleted the external release")
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(DpuOperatorConfigGVK)
	err := cl.Get(context.Background(), types.NamespacedName{Name: "cfg"}, got)
	if err == nil && controllerutil.ContainsFinalizer(got, CleanupFinalizer) {
		t.Fatal("finalizer still present after preserving external release")
	}
}

func TestCleanupReconcileAddsFinalizerOnVendor(t *testing.T) {
	sch := lcmScheme()
	obj := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(obj).WithStatusSubresource(obj).Build()
	r := &LifecycleManager{Client: cl, Scheme: sch, Probe: ReadyClusterProbe()}
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
	if !controllerutil.ContainsFinalizer(got, CleanupFinalizer) {
		t.Fatal("vendor config must get the cleanup finalizer")
	}
}

func TestHelmUninstallRefusesExternalRelease(t *testing.T) {
	mem := memWith(externalRel())
	cfg, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Uninstall(context.Background(), cfg); !errors.Is(err, ErrReleaseUnmanaged) {
		t.Fatalf("uninstall external: %v", err)
	}
	if mem.count() != 1 {
		t.Fatal("uninstall removed an external release")
	}
}

func containsSkip(skipped []string, needle string) bool {
	for _, s := range skipped {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
