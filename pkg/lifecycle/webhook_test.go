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
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateDpuOperatorAdmissionNVIDIAPin(t *testing.T) {
	ok := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	ok.SetAnnotations(map[string]string{VersionAnnotation: NVIDIAChartVersion})
	if err := ValidateDpuOperatorAdmission(ok); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDpuOperatorAdmissionRejectsHelmOverride(t *testing.T) {
	bad := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	bad.SetAnnotations(map[string]string{helmArgsAnnotation: "--set evil=1"})
	if err := ValidateDpuOperatorAdmission(bad); err == nil {
		t.Fatal("helm-args must be denied")
	}
}

func TestValidateDpuOperatorAdmissionRejectsBadNVIDIAVersion(t *testing.T) {
	bad := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	bad.SetAnnotations(map[string]string{VersionAnnotation: "latest"})
	if err := ValidateDpuOperatorAdmission(bad); err == nil {
		t.Fatal("latest must be denied")
	}
}

func TestValidateDpuOperatorAdmissionRejectsUnknownVendor(t *testing.T) {
	bad := newConfig("cfg", map[string]any{"vendor": "acme"})
	if err := ValidateDpuOperatorAdmission(bad); err == nil {
		t.Fatal("unknown vendor must be denied")
	}
}

func TestValidateDpuOperatorAdmissionAllowsEmptyVendor(t *testing.T) {
	empty := newConfig("cfg", map[string]any{"logLevel": int64(2)})
	if err := ValidateDpuOperatorAdmission(empty); err != nil {
		t.Fatal(err)
	}
}

func TestDpuOperatorConfigValidatorHandle(t *testing.T) {
	ok := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	raw, err := ok.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	v := &DpuOperatorConfigValidator{}
	resp := v.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
	}})
	if !resp.Allowed {
		t.Fatalf("create nvidia: %+v", resp)
	}
	del := v.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete,
	}})
	if !del.Allowed {
		t.Fatal("delete must be allowed")
	}
	evil := newConfig("cfg", map[string]any{"vendor": "nvidia"})
	evil.SetAnnotations(map[string]string{"lifecycle.opi.nvidia.com/chart-url": "https://evil.example/chart.tgz"})
	eraw, err := evil.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	denied := v.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: eraw},
	}})
	if denied.Allowed {
		t.Fatal("chart-url update must be denied")
	}
}

func TestMainRegistersDpuOperatorConfigWebhook(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "SetupDpuOperatorConfigWebhook") {
		t.Fatal("cmd/main.go must register the DpuOperatorConfig validating webhook")
	}
}
