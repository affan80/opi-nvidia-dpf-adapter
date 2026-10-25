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
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DpuOperatorConfigValidatePath is the admission URL for DpuOperatorConfig.
const DpuOperatorConfigValidatePath = "/validate-config-openshift-io-v1-dpuoperatorconfig"

// ValidateDpuOperatorAdmission is the NVIDIA pin gate. It rejects Helm/OLM
// overrides and a version pin that is not the allow-list. AMD stays
// detect-only: this does not invent an AMD chart.
func ValidateDpuOperatorAdmission(cfg *unstructured.Unstructured) error {
	if cfg == nil {
		return fmt.Errorf("DpuOperatorConfig is empty")
	}
	if err := RejectInstallOverrides(cfg); err != nil {
		return err
	}
	in := ParseIntent(cfg)
	if !in.Known {
		if raw := ResolveVendor(cfg); raw != "" {
			return fmt.Errorf("vendor %q is not nvidia or amd", raw)
		}
		return nil
	}
	if in.Vendor == VendorNVIDIA && in.PinnedVersion != "" {
		if err := ValidateDesiredVersion(VendorNVIDIA, in.PinnedVersion); err != nil {
			return err
		}
	}
	return nil
}

// DpuOperatorConfigValidator is the validating admission handler.
type DpuOperatorConfigValidator struct{}

// Handle admits or denies one DpuOperatorConfig request.
func (v *DpuOperatorConfigValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("")
	}
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(raw); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := ValidateDpuOperatorAdmission(u); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

// SetupDpuOperatorConfigWebhook registers the validator on the manager
// webhook server. The ValidatingWebhookConfiguration is shipped separately.
func SetupDpuOperatorConfigWebhook(mgr ctrl.Manager) error {
	if mgr == nil || mgr.GetWebhookServer() == nil {
		return fmt.Errorf("webhook server is not configured")
	}
	mgr.GetWebhookServer().Register(DpuOperatorConfigValidatePath, &webhook.Admission{
		Handler: &DpuOperatorConfigValidator{},
	})
	return nil
}
