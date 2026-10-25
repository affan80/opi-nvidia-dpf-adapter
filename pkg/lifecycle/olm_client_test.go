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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func olmScheme() *runtime.Scheme {
	s := lcmScheme()
	s.AddKnownTypeWithName(SubscriptionGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(
		SubscriptionGVK.GroupVersion().WithKind("SubscriptionList"),
		&unstructured.UnstructuredList{},
	)
	s.AddKnownTypeWithName(CSVGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(
		CSVGVK.GroupVersion().WithKind("ClusterServiceVersionList"),
		&unstructured.UnstructuredList{},
	)
	return s
}

func olmSubscription(name, version string, labels map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"name":    name,
			"channel": "stable",
			"source":  "custom",
		},
		"status": map[string]any{
			"installedCSV": name + "." + version,
			"currentCSV":   name + "." + version,
		},
	}}
	obj.SetGroupVersionKind(SubscriptionGVK)
	obj.SetNamespace(DefaultNamespace)
	obj.SetName(name)
	if labels != nil {
		obj.SetLabels(labels)
	}
	return obj
}

func olmCSV(name, version string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"version": version},
	}}
	obj.SetGroupVersionKind(CSVGVK)
	obj.SetNamespace(DefaultNamespace)
	obj.SetName(name + "." + version)
	return obj
}

func TestNVIDIAOLMPackageIsUnpublished(t *testing.T) {
	if NVIDIAOLMPublished {
		t.Fatal("latest DPF docs install via Helm; there is no OperatorHub package")
	}
	c, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		t.Fatal(err)
	}
	if c.OLMPublished || c.OLMPackage != "" || c.OLMChannel != "" || c.OLMCatalog != "" {
		t.Fatalf("nvidia must not pin an OLM catalog: %+v", c)
	}
	amd, err := DefaultInstallConfig(VendorAMD)
	if err != nil {
		t.Fatal(err)
	}
	if amd.OLMPublished || amd.OLMPackage != "" {
		t.Fatalf("amd has no OLM package: %+v", amd)
	}
}

func TestVersionsYAMLPinsOLMUnpublished(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		DPF struct {
			OLM struct {
				Published bool   `json:"published"`
				Package   string `json:"package"`
				Channel   string `json:"channel"`
				Catalog   string `json:"catalog"`
			} `json:"olm"`
		} `json:"dpf"`
	}
	if err := yaml.Unmarshal(b, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.DPF.OLM.Published || pin.DPF.OLM.Package != "" ||
		pin.DPF.OLM.Channel != "" || pin.DPF.OLM.Catalog != "" {
		t.Fatalf("config/versions.yaml must pin olm unpublished: %+v", pin.DPF.OLM)
	}
	if !strings.Contains(string(b), "olm:") {
		t.Fatal("config/versions.yaml must declare the olm pin")
	}
}

func TestRejectOLMOverrides(t *testing.T) {
	for _, key := range []string{
		"lifecycle.opi.nvidia.com/olm-catalog",
		"lifecycle.opi.nvidia.com/olm-package",
		"lifecycle.opi.nvidia.com/subscription",
		"lifecycle.opi.nvidia.com/catalog-source",
	} {
		obj := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
		obj.SetAnnotations(map[string]string{key: "https://evil.example/index.yaml"})
		if err := RejectInstallOverrides(obj); err == nil {
			t.Errorf("%s was accepted", key)
		}
	}
}

func TestClusterOLMClientAbsentAPIs(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).Build()
	got := NewClusterOLMClient(cl, stubMapper{})
	if got.APIsPresent(context.Background()) {
		t.Fatal("operators.coreos.com missing must be absent")
	}
	_, err := got.GetSubscription(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if !errors.Is(err, ErrOLMNotPresent) && !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("err = %v, want ErrOLMNotPresent or ErrSubscriptionNotFound", err)
	}
}

func TestClusterOLMClientDoesNotCreateSubscription(t *testing.T) {
	mapper := stubMapper{present: SubscriptionGVK.GroupKind()}
	cl := fake.NewClientBuilder().WithScheme(olmScheme()).Build()
	olm := NewClusterOLMClient(cl, mapper)
	if !olm.APIsPresent(context.Background()) {
		t.Fatal("mapped Subscription GVK must count as present")
	}
	cfg := mustNVIDIAConfig(t)
	if _, err := olm.Subscribe(context.Background(), cfg); !errors.Is(err, ErrOLMMutationNotConfigured) {
		t.Fatalf("Subscribe = %v, want ErrOLMMutationNotConfigured", err)
	}
	var listed unstructured.UnstructuredList
	listed.SetGroupVersionKind(SubscriptionGVK.GroupVersion().WithKind("SubscriptionList"))
	if err := cl.List(context.Background(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("created %d subscriptions; mutation is forbidden", len(listed.Items))
	}
}

func TestOLMInstallerAbsentAPIsAreNotError(t *testing.T) {
	mem := &MemoryOLMClient{}
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorNVIDIA, Known: true, PinnedVersion: NVIDIAChartVersion,
	})
	if err != nil && !errors.Is(err, ErrOLMNotPresent) {
		t.Fatalf("absent OLM: %v", err)
	}
	if got.Action == InstallInstalled || got.Action == InstallAdopted {
		t.Fatalf("absent OLM must not install: %+v", got)
	}
	if mem.SubscribeCalls != 0 {
		t.Fatalf("Subscribe called %d times on a cluster without OLM", mem.SubscribeCalls)
	}
}

func TestOLMInstallerDoesNotSubscribeWhenAPIsPresent(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	_, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorNVIDIA, Known: true,
	})
	if err != nil && !errors.Is(err, ErrOLMNotPresent) &&
		!errors.Is(err, ErrOLMMutationNotConfigured) &&
		!errors.Is(err, ErrOLMPackageNotPublished) {
		t.Fatalf("no subscription: %v", err)
	}
	if mem.SubscribeCalls != 0 {
		t.Fatal("unpublished DPF package must not create a Subscription")
	}
}

func TestOLMInstallerObservesCSVVersion(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	mem.Put(OLMSubscription{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Package:   NVIDIAReleaseName,
		Version:   NVIDIAChartVersion,
		CSV:       NVIDIAReleaseName + "." + NVIDIAChartVersion,
		Labels:    map[string]string{"owner": "olm"},
	})
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorNVIDIA, Known: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUnmanaged || got.Version != NVIDIAChartVersion {
		t.Fatalf("observe = %+v, want unmanaged/%s", got, NVIDIAChartVersion)
	}
	if mem.SubscribeCalls != 0 {
		t.Fatal("observe must not subscribe")
	}
}

func TestOLMInstallerDoesNotAdoptWithoutConsent(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	mem.Put(OLMSubscription{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Package:   NVIDIAReleaseName,
		Version:   NVIDIAChartVersion,
		CSV:       NVIDIAReleaseName + "." + NVIDIAChartVersion,
	})
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorNVIDIA, Known: true, AdoptExisting: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallUnmanaged {
		t.Fatalf("action = %s, want unmanaged", got.Action)
	}
	sub, err := mem.GetSubscription(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ManagedByAdapter() {
		t.Fatal("silent adopt is forbidden")
	}
}

func TestOLMInstallerAdoptsPinnedSubscription(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	mem.Put(OLMSubscription{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Package:   NVIDIAReleaseName,
		Version:   NVIDIAChartVersion,
		CSV:       NVIDIAReleaseName + "." + NVIDIAChartVersion,
	})
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorNVIDIA, Known: true, AdoptExisting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != InstallAdopted || got.Version != NVIDIAChartVersion {
		t.Fatalf("adopt = %+v, want adopted/%s", got, NVIDIAChartVersion)
	}
	sub, err := mem.GetSubscription(context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.ManagedByAdapter() || sub.Labels[ReleaseModeLabel] != ReleaseModeAdopted {
		t.Fatalf("after claim %+v", sub.Labels)
	}
	if mem.SubscribeCalls != 0 {
		t.Fatal("claim must not create a second Subscription")
	}
}

func TestOLMInstallerSkipsAMD(t *testing.T) {
	mem := &MemoryOLMClient{Present: true}
	got, err := (OLMInstaller{Client: mem}).Ensure(context.Background(), Intent{
		Vendor: VendorAMD, Known: true,
	})
	if !errors.Is(err, ErrNoVendorChart) {
		t.Fatalf("err = %v, want ErrNoVendorChart", err)
	}
	if got.Action != InstallSkipped {
		t.Fatalf("amd = %+v, want skipped", got)
	}
	if mem.SubscribeCalls != 0 {
		t.Fatal("amd has no OLM package")
	}
}

func TestEnsureInstalledOLMCSVVersionWithoutAnnotation(t *testing.T) {
	obj := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	mem := &MemoryOLMClient{Present: true}
	mem.Put(OLMSubscription{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Package:   NVIDIAReleaseName,
		Version:   NVIDIAChartVersion,
		CSV:       NVIDIAReleaseName + "." + NVIDIAChartVersion,
	})
	out, err := (&LifecycleManager{
		OLM: mem, Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Version != NVIDIAChartVersion {
		t.Fatalf("version = %q, want CSV %s without the annotation", out.Install.Version, NVIDIAChartVersion)
	}
	if out.Policy.Skew != SkewSupported {
		t.Fatalf("skew = %s, want Supported from the CSV version", out.Policy.Skew)
	}
}

func TestEnsureInstalledHelmWinsOverOLM(t *testing.T) {
	sec := helmStorageSecret(t, NVIDIAChartVersion, map[string]string{"owner": "helm"})
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(sec, obj).Build()
	olm := &MemoryOLMClient{Present: true}
	olm.Put(OLMSubscription{
		Name:      NVIDIAReleaseName,
		Namespace: DefaultNamespace,
		Version:   "v99.0.0",
		CSV:       NVIDIAReleaseName + ".v99.0.0",
	})
	out, err := (&LifecycleManager{
		Client: cl, Helm: NewSecretHelmClient(cl), OLM: olm, Probe: ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallUnmanaged || out.Install.Version != NVIDIAChartVersion {
		t.Fatalf("helm must win on this lab: %+v", out.Install)
	}
	if olm.SubscribeCalls != 0 {
		t.Fatal("Helm storage present must not drive OLM subscribe")
	}
}

func TestEnsureInstalledOLMAbsentFallsBackToDetect(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(lcmScheme()).WithObjects(obj).Build()
	out, err := (&LifecycleManager{
		Client: cl,
		Helm:   NewSecretHelmClient(cl),
		OLM:    NewClusterOLMClient(cl, stubMapper{}),
		Probe:  ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Install.Action != InstallAbsent && out.Install.Action != InstallDetected {
		t.Fatalf("no Helm + no OLM = %+v, want detect fallback", out.Install)
	}
}

func TestMainWiresOLMClient(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "lifecycle.NewClusterOLMClient") {
		t.Fatal("cmd/main.go must wire NewClusterOLMClient onto LifecycleManager.OLM")
	}
	if strings.Contains(src, "MemoryOLMClient") {
		t.Fatal("cmd/main.go must not use the in-process test OLM client")
	}
}

func TestOLMClientSourceHasNoOperatorSDK(t *testing.T) {
	b, err := os.ReadFile("olm_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{
		"github.com/operator-framework",
		"exec.Command",
		"os/exec",
		"CatalogSource",
		"OperatorGroup",
	} {
		if strings.Contains(src, banned) {
			t.Errorf("olm_client.go contains %q", banned)
		}
	}
}

func TestManagerRoleObservesOLMNoCreate(t *testing.T) {
	role := loadManagerRole(t)
	if !roleHas(role, "operators.coreos.com", "subscriptions", []string{"get", "list", "update", "patch"}) {
		t.Fatal("manager-role must get/list/update/patch subscriptions to claim a matching pin")
	}
	if !roleHas(role, "operators.coreos.com", "clusterserviceversions", []string{"get", "list"}) {
		t.Fatal("manager-role must get/list CSVs to read the installed version")
	}
	for _, rule := range role.Rules {
		match := false
		for _, g := range rule.APIGroups {
			if g == "operators.coreos.com" {
				match = true
			}
		}
		if !match {
			continue
		}
		for _, v := range rule.Verbs {
			if v == "create" || v == "delete" || v == "deletecollection" {
				t.Fatalf("OLM resources must not create or delete: %+v", rule)
			}
		}
	}
}

func TestClusterOLMClientReadsCSVVersion(t *testing.T) {
	sub := olmSubscription(NVIDIAReleaseName, NVIDIAChartVersion, map[string]string{"owner": "olm"})
	csv := olmCSV(NVIDIAReleaseName, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(olmScheme()).WithObjects(sub, csv).Build()
	mapper := stubMapper{present: SubscriptionGVK.GroupKind()}
	got, err := NewClusterOLMClient(cl, mapper).GetSubscription(
		context.Background(), DefaultNamespace, NVIDIAReleaseName)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != NVIDIAChartVersion {
		t.Fatalf("version = %q, want %s from CSV", got.Version, NVIDIAChartVersion)
	}
	if got.ManagedByAdapter() {
		t.Fatal("owner=olm must stay unmanaged")
	}
}

func TestStubMapperRecognizesOLMGroupKind(t *testing.T) {
	// Compile-time reminder: Subscription GVK must stay operators.coreos.com.
	if SubscriptionGVK.Group != "operators.coreos.com" || SubscriptionGVK.Kind != "Subscription" {
		t.Fatalf("Subscription GVK drifted: %+v", SubscriptionGVK)
	}
	if CSVGVK.Kind != "ClusterServiceVersion" {
		t.Fatalf("CSV GVK drifted: %+v", CSVGVK)
	}
	_ = schema.GroupKind{}
	_ = meta.RESTMapping{}
	_ = client.ObjectKey{}
}
