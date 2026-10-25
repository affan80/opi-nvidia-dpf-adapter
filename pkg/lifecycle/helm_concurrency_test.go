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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type countingUpdateClient struct {
	client.Client
	updates atomic.Int32
}

func (c *countingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates.Add(1)
	return c.Client.Update(ctx, obj, opts...)
}

func TestConcurrentClusterHelmInstallOneWinner(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	helm.ChartRoot = probeChartRoot(t)
	cfg := ProbeHelmConfig("0.1.0")

	var wins, errN int32
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, err := helm.Install(context.Background(), cfg, nil)
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.Is(err, ErrReleaseExists):
			default:
				atomic.AddInt32(&errN, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || errN != 0 {
		t.Fatalf("wins=%d err=%d, want 1 winner and 0 conflicts", wins, errN)
	}
	rel, err := helm.GetRelease(context.Background(), HelmProbeNamespace, HelmProbeReleaseName)
	if err != nil || rel.Revision != 1 {
		t.Fatalf("release = %+v err=%v", rel, err)
	}
	var secs corev1.SecretList
	if err := cl.List(context.Background(), &secs, client.InNamespace(HelmProbeNamespace)); err != nil {
		t.Fatal(err)
	}
	if len(secs.Items) != 1 {
		t.Fatalf("helm secrets = %d, want 1", len(secs.Items))
	}
}

func TestConcurrentClusterHelmUpgradeNoConflict(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	helm := NewClusterHelmClient(cl)
	helm.ChartRoot = probeChartRoot(t)
	ctx := context.Background()
	if _, err := helm.Install(ctx, ProbeHelmConfig("0.1.0"), nil); err != nil {
		t.Fatal(err)
	}
	cfg := ProbeHelmConfig("0.2.0")
	var errN int32
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if _, err := helm.Upgrade(ctx, cfg, nil); err != nil {
				atomic.AddInt32(&errN, 1)
			}
		}()
	}
	wg.Wait()
	if errN != 0 {
		t.Fatalf("Upgrade err=%d, want 0 (409 must retry under the release lock)", errN)
	}
	rel, err := helm.GetRelease(ctx, HelmProbeNamespace, HelmProbeReleaseName)
	if err != nil || rel.ChartVersion != "0.2.0" || rel.Revision < 2 {
		t.Fatalf("after concurrent upgrade %+v err=%v", rel, err)
	}
	cm := &corev1.ConfigMap{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: HelmProbeNamespace, Name: HelmProbeReleaseName}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["version"] != "0.2.0" {
		t.Fatalf("configmap version %q", cm.Data["version"])
	}
}

func TestClaimReleaseIsIdempotent(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{
		"name": NVIDIAReleaseName, "owner": "helm", "status": "deployed", "version": "1",
	})
	inner := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec).Build()
	cl := &countingUpdateClient{Client: inner}
	helm := NewSecretHelmClient(cl)
	cfg, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	first, err := helm.ClaimRelease(context.Background(), cfg)
	if err != nil || !first.ManagedByAdapter() {
		t.Fatalf("first claim %+v err=%v", first, err)
	}
	if cl.updates.Load() != 1 {
		t.Fatalf("first claim updates=%d, want 1", cl.updates.Load())
	}
	second, err := helm.ClaimRelease(context.Background(), cfg)
	if err != nil || !second.ManagedByAdapter() {
		t.Fatalf("second claim %+v err=%v", second, err)
	}
	if cl.updates.Load() != 1 {
		t.Fatalf("second claim wrote again updates=%d, want 1", cl.updates.Load())
	}
}

func TestClaimReleaseRefusesWrongNVIDIAVersion(t *testing.T) {
	sec := helmStorageSecret(t, "v26.3.9", map[string]string{
		"name": NVIDIAReleaseName, "owner": "helm", "status": "deployed", "version": "1",
	})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec).Build()
	helm := NewSecretHelmClient(cl)
	cfg, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := helm.ClaimRelease(context.Background(), cfg)
	if !errors.Is(err, ErrReleaseUnmanaged) {
		t.Fatalf("wrong pin claim = %+v err=%v", rel, err)
	}
	got := &corev1.Secret{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(sec), got); err != nil {
		t.Fatal(err)
	}
	if got.Labels[ReleaseOwnerLabel] == ReleaseOwner {
		t.Fatal("wrong pin must not stamp adapter owner")
	}
}

func TestConcurrentEnsureInstalledAdoptExisting(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{
		"name": NVIDIAReleaseName, "owner": "helm", "status": "deployed", "version": "1",
	})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec).Build()
	helm := NewSecretHelmClient(cl)
	mgr := &LifecycleManager{Helm: helm, Probe: ReadyClusterProbe()}
	var errN int32
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			cfg := newConfig(fmt.Sprintf("user-%d", i), map[string]any{"vendor": "nvidia"})
			cfg.SetAnnotations(map[string]string{
				VersionAnnotation:       NVIDIAChartVersion,
				AdoptExistingAnnotation: "true",
			})
			out, err := mgr.EnsureInstalled(context.Background(), cfg)
			if err != nil {
				atomic.AddInt32(&errN, 1)
				return
			}
			if out.Install.Action != InstallAdopted && out.Install.Action != InstallNoop {
				atomic.AddInt32(&errN, 1)
			}
		}(i)
	}
	wg.Wait()
	if errN != 0 {
		t.Fatalf("adopt EnsureInstalled err=%d", errN)
	}
	rel, err := helm.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil || !rel.ManagedByAdapter() {
		t.Fatalf("after adopt %+v err=%v", rel, err)
	}
	var listed corev1.SecretList
	if err := cl.List(context.Background(), &listed); err != nil {
		t.Fatal(err)
	}
	nSecrets := 0
	for i := range listed.Items {
		if listed.Items[i].Type == helmStorageSecretType {
			nSecrets++
		}
	}
	if nSecrets != 1 {
		t.Fatalf("secrets=%d, claim must not create a second dpf-operator", nSecrets)
	}
}

func TestConcurrentClaimReleaseSameSecretSucceeds(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{
		"name": NVIDIAReleaseName, "owner": "helm", "status": "deployed", "version": "1",
	})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec).Build()
	helm := NewSecretHelmClient(cl)
	cfg, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	var errN int32
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			rel, err := helm.ClaimRelease(context.Background(), cfg)
			if err != nil || !rel.ManagedByAdapter() {
				atomic.AddInt32(&errN, 1)
			}
		}()
	}
	wg.Wait()
	if errN != 0 {
		t.Fatalf("ClaimRelease err=%d, want 0", errN)
	}
	rel, err := helm.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil || !rel.ManagedByAdapter() {
		t.Fatalf("after claim %+v err=%v", rel, err)
	}
}
