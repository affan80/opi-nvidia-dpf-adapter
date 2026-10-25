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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func helmStorageSecret(t *testing.T, chartVersion string, releaseLabels map[string]string) *corev1.Secret {
	t.Helper()
	doc := helmStorageRelease{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Version:   1,
		Info:      helmStorageInfo{Status: HelmStatusDeployed},
		Chart:     helmStorageChart{Metadata: helmStorageMeta{Name: NVIDIAChartName, Version: chartVersion}},
		Labels:    releaseLabels,
	}
	payload, err := encodeHelmStorageRelease(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1.dpf-operator.v1",
			Namespace: DefaultNamespace,
			Labels: map[string]string{
				"name":    NVIDIAReleaseName,
				"owner":   "helm",
				"status":  HelmStatusDeployed,
				"version": "1",
			},
		},
		Type: helmStorageSecretType,
		Data: map[string][]byte{"release": []byte(payload)},
	}
}

func TestSecretHelmClientReadsUnmanagedLabRelease(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).
		WithObjects(helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})).
		Build()
	got, err := NewSecretHelmClient(cl).GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity() != "dpf-operator-system/dpf-operator@v26.4.0" {
		t.Fatalf("identity = %s", got.Identity())
	}
	if got.ManagedByAdapter() {
		t.Fatal("owner=helm must stay unmanaged")
	}
	if got.Status != HelmStatusDeployed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestSecretHelmClientReadsAdapterOwnerFromPayload(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).
		WithObjects(helmStorageSecret(t, NVIDIAChartVersion, map[string]string{ReleaseOwnerLabel: ReleaseOwner})).
		Build()
	got, err := NewSecretHelmClient(cl).GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ManagedByAdapter() {
		t.Fatal("payload owner label must adopt")
	}
}

func TestSecretHelmClientMissingRelease(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	_, err := NewSecretHelmClient(cl).GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("err = %v, want ErrReleaseNotFound", err)
	}
}

func TestSecretHelmClientDoesNotMutate(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec).Build()
	helm := NewSecretHelmClient(cl)
	cfg := mustNVIDIAConfig(t)

	_, err := helm.Install(context.Background(), cfg, SafeHelmValues(cfg))
	if !errors.Is(err, ErrHelmMutationNotConfigured) {
		t.Fatalf("Install = %v, want ErrHelmMutationNotConfigured", err)
	}
	if _, err := helm.Upgrade(context.Background(), cfg, SafeHelmValues(cfg)); !errors.Is(err, ErrReleaseUnmanaged) {
		t.Fatalf("Upgrade unmanaged = %v, want ErrReleaseUnmanaged", err)
	}
	if _, err := helm.Uninstall(context.Background(), cfg); !errors.Is(err, ErrReleaseUnmanaged) {
		t.Fatalf("Uninstall unmanaged = %v, want ErrReleaseUnmanaged", err)
	}

	var listed corev1.SecretList
	if err := cl.List(context.Background(), &listed, client.InNamespace(DefaultNamespace)); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("secrets = %d, mutation must not create or delete Helm storage", len(listed.Items))
	}
}

func TestEnsureInstalledSecretHelmReportsUnmanaged(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec, obj).Build()
	out, err := (&LifecycleManager{
		Client: cl, Helm: NewSecretHelmClient(cl), Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallUnmanaged || !out.Install.Present {
		t.Fatalf("install = %+v, want unmanaged/present", out.Install)
	}
}

func TestEnsureInstalledSecretHelmClaimsMatchingPin(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	obj.SetAnnotations(map[string]string{
		VersionAnnotation:       NVIDIAChartVersion,
		AdoptExistingAnnotation: "true",
	})
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec, obj).Build()
	helm := NewSecretHelmClient(cl)
	out, err := (&LifecycleManager{
		Client: cl, Helm: helm, Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallAdopted || !out.Install.Present {
		t.Fatalf("claim = %+v, want adopted/present", out.Install)
	}
	rel, err := helm.GetRelease(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if !rel.ManagedByAdapter() || rel.Labels[helmStorageOwnerLabel] != "helm" {
		t.Fatalf("after claim %+v", rel.Labels)
	}
	var listed corev1.SecretList
	if err := cl.List(context.Background(), &listed); err != nil {
		t.Fatal(err)
	}
	secrets := 0
	for i := range listed.Items {
		if listed.Items[i].Type == helmStorageSecretType {
			secrets++
		}
	}
	if secrets != 1 {
		t.Fatalf("helm secrets = %d, claim must not create a second release", secrets)
	}
}

func TestEnsureInstalledSecretHelmMissingFallsBackToDetect(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(obj).Build()
	out, err := (&LifecycleManager{
		Client: cl, Helm: NewSecretHelmClient(cl),
		Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallAbsent && out.Install.Action != InstallDetected {
		t.Fatalf("missing Helm storage = %+v, want detect fallback not install/fail", out.Install)
	}
}

func TestMainWiresSecretHelmClient(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "lifecycle.NewClusterHelmClient") &&
		!strings.Contains(src, "lifecycle.NewSecretHelmClient") {
		t.Fatal("cmd/main.go must wire a Secret/Cluster Helm client onto LifecycleManager.Helm")
	}
	if strings.Contains(src, "MemoryHelmClient") {
		t.Fatal("cmd/main.go must not use the in-process test Helm client")
	}
}

func TestMainDisablesSecretInformer(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "DisableFor") || !strings.Contains(src, "corev1.Secret") {
		t.Fatal("cmd/main.go must DisableFor corev1.Secret so the cache does not cluster-watch Helm storage")
	}
	if strings.Contains(src, "resources=secrets,verbs=") && strings.Contains(src, "watch") {
		t.Fatal("main.go must not grant a secrets watch")
	}
}

func TestSecretHelmClientSourceHasNoCLIOrHelmSDK(t *testing.T) {
	b, err := os.ReadFile("secret_helm_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"helm.New", "exec.Command", "os/exec", "\"helm.sh/helm"} {
		if strings.Contains(src, banned) {
			t.Errorf("secret_helm_client.go contains %q", banned)
		}
	}
}

func TestManagerRoleListsHelmSecrets(t *testing.T) {
	if !roleHas(loadManagerRole(t), "", "secrets", []string{"get", "list", "update", "patch"}) {
		t.Fatal("manager-role must get/list/update/patch secrets to claim a matching pin")
	}
	role := loadManagerRole(t)
	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			if res != "secrets" {
				continue
			}
			for _, v := range rule.Verbs {
				if v == "create" || v == "delete" || v == "deletecollection" || v == "watch" {
					t.Fatalf("secrets must not create, delete, or watch Helm storage: %+v", rule)
				}
			}
		}
	}
}
