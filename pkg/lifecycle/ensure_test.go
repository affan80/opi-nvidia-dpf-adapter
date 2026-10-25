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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type recordingInstaller struct {
	calls  []Intent
	result InstallResult
	err    error
}

func (r *recordingInstaller) Ensure(_ context.Context, in Intent) (InstallResult, error) {
	r.calls = append(r.calls, in)
	return r.result, r.err
}

// TestReconcileCallsEnsureInstalled is TASK 02.01: Reconcile must run the
// install flow, not only log "would install".
func TestReconcileCallsEnsureInstalled(t *testing.T) {
	sch := lcmScheme()
	obj := configWithVersion(VendorNVIDIA, "v26.4.0")
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(obj).WithStatusSubresource(obj).Build()
	rec := &recordingInstaller{result: InstallResult{
		Action: InstallDetected, Present: true, Version: "v26.4.0",
	}}
	r := &LifecycleManager{Client: cl, Scheme: sch, Installer: rec, Probe: ReadyClusterProbe()}

	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cfg"},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("installer.Ensure calls = %d, want 1 (EnsureInstalled was skipped)", len(rec.calls))
	}
	if rec.calls[0].Vendor != VendorNVIDIA {
		t.Errorf("intent vendor = %q, want %s", rec.calls[0].Vendor, VendorNVIDIA)
	}
	if rec.calls[0].PinnedVersion != "v26.4.0" {
		t.Errorf("pinned version = %q, want v26.4.0", rec.calls[0].PinnedVersion)
	}
	if rec.calls[0].Source != IntentSpec {
		t.Errorf("source = %q, want spec", rec.calls[0].Source)
	}
}

func TestParseIntentAdoptExisting(t *testing.T) {
	off := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	if ParseIntent(off).AdoptExisting {
		t.Fatal("adopt-existing defaults to false so lab owner=helm stays unmanaged")
	}
	on := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	on.SetAnnotations(map[string]string{AdoptExistingAnnotation: "true"})
	if !ParseIntent(on).AdoptExisting {
		t.Fatal("adopt-existing=true must be parsed")
	}
}

func TestParseIntent(t *testing.T) {
	specWins := newConfig("cfg", map[string]any{"vendor": VendorNVIDIA})
	specWins.SetAnnotations(map[string]string{VendorAnnotation: VendorAMD})
	annOnly := newConfig("cfg", map[string]any{})
	annOnly.SetAnnotations(map[string]string{
		VendorAnnotation:  VendorNVIDIA,
		VersionAnnotation: "v26.4.0",
	})
	evil := newConfig("cfg", map[string]any{})
	evil.SetAnnotations(map[string]string{VendorAnnotation: "../../../etc/passwd"})

	cases := []struct {
		name   string
		cfg    *unstructured.Unstructured
		vendor string
		source IntentSource
		known  bool
		pin    string
	}{
		{"nil", nil, "", IntentNone, false, ""},
		{"empty", newConfig("cfg", map[string]any{"logLevel": int64(2)}), "", IntentNone, false, ""},
		{"spec wins", specWins, VendorNVIDIA, IntentSpec, true, ""},
		{"annotation fallback", annOnly, VendorNVIDIA, IntentAnnotation, true, "v26.4.0"},
		{"injection is invalid", evil, invalidVendor, IntentAnnotation, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseIntent(tc.cfg)
			if got.Vendor != tc.vendor || got.Source != tc.source || got.Known != tc.known || got.PinnedVersion != tc.pin {
				t.Errorf("ParseIntent = %+v, want vendor=%s source=%s known=%v pin=%s",
					got, tc.vendor, tc.source, tc.known, tc.pin)
			}
		})
	}
}

func TestRunPreflight(t *testing.T) {
	skip := RunPreflight(Intent{})
	if !skip.Skip || !skip.OK {
		t.Errorf("empty intent: %+v, want skip+ok", skip)
	}
	reject := RunPreflight(Intent{Vendor: "acme", Known: false})
	if reject.OK || reject.Reason != SkewUnknownVendor {
		t.Errorf("unknown vendor: %+v, want reject UnknownVendor", reject)
	}
	ok := RunPreflight(Intent{Vendor: VendorNVIDIA, Known: true})
	if !ok.OK || ok.Skip {
		t.Errorf("known vendor: %+v, want ok", ok)
	}
}

func TestEvaluatePolicyUsesPinnedWindow(t *testing.T) {
	got := EvaluatePolicy(VendorNVIDIA, "v26.4.0")
	if got.Skew != SkewSupported || got.Version != "v26.4.0" {
		t.Errorf("policy = %+v, want Supported/v26.4.0", got)
	}
	if got.Range.MinInclusive != nvidiaDPFMin || got.Range.MaxExclusive != nvidiaDPFMax {
		t.Errorf("range = %+v, want [%s, %s)", got.Range, nvidiaDPFMin, nvidiaDPFMax)
	}
}

func TestAssessReadiness(t *testing.T) {
	in := Intent{Vendor: VendorNVIDIA, Known: true}
	present := InstallResult{Present: true, Action: InstallDetected}
	healthy := OperatorStatus{Phase: OperatorReady, Ready: true}
	if got := AssessReadiness(in, present, PolicyResult{Skew: SkewSupported}, healthy); !got.Ready {
		t.Errorf("supported+present+healthy: %+v, want ready", got)
	}
	if got := AssessReadiness(in, present, PolicyResult{Skew: SkewSupported}, OperatorStatus{}); got.Ready {
		t.Errorf("supported+present without observation: %+v, want not ready", got)
	}
	if got := AssessReadiness(in, InstallResult{}, PolicyResult{Skew: SkewSupported}, healthy); got.Ready {
		t.Errorf("supported+absent: %+v, want not ready", got)
	}
	if got := AssessReadiness(in, present, PolicyResult{Skew: SkewTooOld}, healthy); got.Ready {
		t.Errorf("too old: %+v, want not ready", got)
	}
	if got := AssessReadiness(in, present, PolicyResult{Skew: SkewBlocked}, healthy); got.Ready {
		t.Errorf("blocked: %+v, want not ready", got)
	}
	if got := AssessReadiness(Intent{Vendor: "acme"}, present, PolicyResult{Skew: SkewUnknownVendor}, healthy); got.Ready {
		t.Errorf("unknown vendor: %+v, want not ready", got)
	}
}

type stubMapper struct {
	present schema.GroupKind
}

func (s stubMapper) RESTMapping(gk schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	if gk == s.present {
		return &meta.RESTMapping{GroupVersionKind: gk.WithVersion("v1alpha1")}, nil
	}
	return nil, &meta.NoKindMatchError{GroupKind: gk}
}

func TestDetectingInstallerReportsPresentWhenMapped(t *testing.T) {
	got, err := DetectingInstaller{Mapper: stubMapper{present: nvidiaProbeGVK.GroupKind()}}.Ensure(
		context.Background(), Intent{Vendor: VendorNVIDIA, PinnedVersion: "v26.4.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Action != InstallDetected {
		t.Errorf("got %+v, want detected/present", got)
	}
	if got.Version != "v26.4.0" {
		t.Errorf("version = %q, want the pin (installer does not invent one)", got.Version)
	}
}

func TestDetectingInstallerAbsentWithoutMapping(t *testing.T) {
	got, err := DetectingInstaller{Mapper: stubMapper{}}.Ensure(
		context.Background(), Intent{Vendor: VendorNVIDIA})
	if err != nil {
		t.Fatal(err)
	}
	if got.Present || got.Action != InstallAbsent {
		t.Errorf("got %+v, want absent", got)
	}
}

// AMD on a BlueField cluster must stay Absent. Applying stub DSC CRDs
// would make DetectingInstaller report present and look like a real card.
func TestDetectingInstallerAMDAbsentWithoutCRD(t *testing.T) {
	got, err := DetectingInstaller{Mapper: stubMapper{}}.Ensure(
		context.Background(), Intent{Vendor: VendorAMD})
	if err != nil {
		t.Fatal(err)
	}
	if got.Present || got.Action != InstallAbsent {
		t.Errorf("AMD without DSC CRD: %+v, want absent (do not install stubs on BlueField)", got)
	}
}

func TestDetectingInstallerNeverMutates(t *testing.T) {
	var _ Installer = DetectingInstaller{}
	b, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"helm.New", "exec.Command(\"helm\"", "HelmClient"} {
		if strings.Contains(src, banned) {
			t.Errorf("installer.go contains %q; DetectingInstaller must not mutate the vendor install", banned)
		}
	}
}

func TestEnsureInstalledSkipsInstallerWhenNoVendor(t *testing.T) {
	rec := &recordingInstaller{}
	r := &LifecycleManager{Installer: rec}
	out, err := r.EnsureInstalled(context.Background(), newConfig("cfg", map[string]any{"logLevel": int64(2)}))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Preflight.Skip || out.Install.Action != InstallSkipped {
		t.Errorf("got %+v, want skip", out)
	}
	if len(rec.calls) != 0 {
		t.Errorf("installer called %d times on an empty intent", len(rec.calls))
	}
}

func TestEnsureInstalledSkipsInstallerForUnknownVendor(t *testing.T) {
	rec := &recordingInstaller{}
	obj := newConfig("cfg", map[string]any{})
	obj.SetAnnotations(map[string]string{VendorAnnotation: "acme"})
	r := &LifecycleManager{Installer: rec}
	out, err := r.EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if out.Policy.Skew != SkewUnknownVendor || out.Install.Action != InstallSkipped {
		t.Errorf("got %+v, want skipped UnknownVendor", out)
	}
	if len(rec.calls) != 0 {
		t.Errorf("installer ran for a rejected vendor")
	}
}
