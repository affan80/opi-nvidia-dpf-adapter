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

// Package lifecycle holds the Lifecycle Manager (LCM) of the Hybrid pattern.
// The LCM watches DpuOperatorConfig and is responsible for installing and
// pinning the requested vendor's operator (e.g. NVIDIA DPF) via Helm/OLM. This
// phase ships a lightweight placeholder that reads the requested vendor and
// logs the intent; the Helm/OLM wiring lands in a later phase.
package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DpuOperatorConfigGVK is the cluster-scoped config the LCM reconciles.
var DpuOperatorConfigGVK = schema.GroupVersionKind{
	Group:   "config.openshift.io",
	Version: "v1",
	Kind:    "DpuOperatorConfig",
}

const (
	vendorKey = "vendor"
	statusKey = "status"
)

// vendorField is the constrained enablement field on DpuOperatorConfig
// (enum nvidia|amd on the pinned OPI fixture). It wins over the annotation.
var vendorField = []string{"spec", vendorKey}

// VendorAnnotation is the dual enablement path. spec.vendor is now on the
// pinned CRD; the annotation stays so clusters that have not rolled the
// fixture still enable NVIDIA. Both paths go through SanitizeVendorToken.
const VendorAnnotation = "lifecycle.opi.nvidia.com/vendor"

// VersionAnnotation declares the installed vendor-operator version when the
// LCM cannot discover it itself (no Helm/OLM wiring yet). It is the operator's
// override and keeps precedence once that wiring lands, because a human
// pinning a version should beat auto-detection.
const VersionAnnotation = "lifecycle.opi.nvidia.com/vendor-operator-version"

// SupportedConditionType is the condition the LCM owns on DpuOperatorConfig.
//
// Single-writer status model: it is NOT "Ready". The dpu-operator daemon owns
// Ready on DpuOperatorConfig (plugin.ReadyConditionType, set in
// internal/daemon/daemon.go) and Ready is that CRD's printer column, so a
// second writer would both flap the condition and misreport the cluster. This
// mirrors the rule mapping.ReservedConditionType enforces for mapping
// documents; see docs/multi-vendor.md section 3.
const SupportedConditionType = "VendorOperatorSupported"

// LifecycleManager installs and pins the requested vendor's operator. In this
// phase it only logs intent; it is the placeholder for the Helm/OLM logic.
type LifecycleManager struct {
	client.Client
	Scheme *runtime.Scheme
	// Now supplies the clock for condition timestamps. Nil means time.Now.
	// It is injectable because lastTransitionTime is formatted to RFC3339,
	// which has second precision: two reconciles in the same second produce
	// an identical string, so without a controllable clock a test cannot tell
	// a preserved timestamp from a rewritten one.
	Now func() time.Time
}

func (r *LifecycleManager) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/status,verbs=get;update;patch

func (r *LifecycleManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("lifecycle")

	cfg := &unstructured.Unstructured{}
	cfg.SetGroupVersionKind(DpuOperatorConfigGVK)
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// spec.vendor first (enum on the pinned CRD), annotation as the
	// fallback. Both are sanitized: a path, URL, or shell fragment is
	// not a vendor name and becomes UnknownVendor.
	vendor := ResolveVendor(cfg)
	if vendor == "" {
		log.Info("DpuOperatorConfig has no requested vendor; nothing to install",
			"config", req.NamespacedName,
			"hint", "set spec.vendor or the "+VendorAnnotation+" annotation to nvidia or amd")
		return ctrl.Result{}, nil
	}
	if !KnownVendor(vendor) {
		log.Info("rejecting unknown vendor",
			vendorKey, vendor, //nolint:logcheck // vendorKey is the enablement field name
			"allowed", strings.Join(KnownVendors, ","),
			"config", req.NamespacedName)
		if err := r.setSkewCondition(ctx, cfg, vendor, "", SkewUnknownVendor); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Placeholder for the real Helm/OLM install-and-pin logic.
	log.Info("would install and pin the vendor operator (placeholder for Helm/OLM)",
		vendorKey, vendor, //nolint:logcheck // vendorKey is the enablement field name
		"config", req.NamespacedName,
		"action", "ensure-installed-and-pinned")

	// Version skew is reported whether or not the install logic exists yet: the
	// adapter writes the vendor's CRs, so the vendor operator's API is a hard
	// dependency and a mismatch surfaces only as children that never go ready.
	installed := cfg.GetAnnotations()[VersionAnnotation]
	skew := ClassifySkew(vendor, installed)
	log.Info("vendor operator version skew", vendorKey, vendor, //nolint:logcheck // vendorKey is the enablement field name
		"installed", installed, "skew", string(skew))

	if err := r.setSkewCondition(ctx, cfg, vendor, installed, skew); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// setSkewCondition upserts the LCM's own condition by type, preserving
// lastTransitionTime while the status is unchanged so the field keeps the
// meaning metav1 gives it.
func (r *LifecycleManager) setSkewCondition(
	ctx context.Context, cfg *unstructured.Unstructured, vendor, installed string, skew Skew,
) error {
	status, message := skewStatus(vendor, installed, skew)

	conds, _, err := unstructured.NestedSlice(cfg.Object, statusKey, "conditions")
	if err != nil {
		return fmt.Errorf("reading status.conditions: %w", err)
	}

	next := map[string]any{
		"type":               SupportedConditionType,
		statusKey:            status,
		"reason":             string(skew),
		"message":            message,
		"lastTransitionTime": r.now().UTC().Format(time.RFC3339),
	}

	replaced := false
	for i, raw := range conds {
		c, ok := raw.(map[string]any)
		if !ok || c["type"] != SupportedConditionType {
			continue
		}
		if c[statusKey] == status && c["reason"] == string(skew) && c["message"] == message {
			// Nothing changed: leave lastTransitionTime and the apiserver alone.
			return nil
		}
		if c[statusKey] == status {
			next["lastTransitionTime"] = c["lastTransitionTime"]
		}
		conds[i] = next
		replaced = true
		break
	}
	if !replaced {
		conds = append(conds, next)
	}

	if err := unstructured.SetNestedSlice(cfg.Object, conds, statusKey, "conditions"); err != nil {
		return fmt.Errorf("setting status.conditions: %w", err)
	}
	return r.Status().Update(ctx, cfg)
}

// skewStatus maps a classification onto a condition status and message.
// Only TooOld and TooNew are False: "the version could not be determined" and
// "this vendor has no pinned window" are Unknown, because reporting those as a
// failure would train operators to ignore the condition.
func skewStatus(vendor, installed string, skew Skew) (status, message string) {
	r, _ := SupportedRangeFor(vendor)
	switch skew {
	case SkewSupported:
		return string(metav1.ConditionTrue), fmt.Sprintf("%s operator %s is within the supported range [%s, %s)",
			vendor, installed, r.MinInclusive, r.MaxExclusive)
	case SkewTooOld:
		return string(metav1.ConditionFalse), fmt.Sprintf("%s operator %s predates the supported range [%s, %s); "+
			"expect fields this adapter writes to be missing",
			vendor, installed, r.MinInclusive, r.MaxExclusive)
	case SkewTooNew:
		return string(metav1.ConditionFalse), fmt.Sprintf("%s operator %s is newer than the supported range [%s, %s); "+
			"its CRD schema may have moved since this adapter was tested",
			vendor, installed, r.MinInclusive, r.MaxExclusive)
	case SkewUnpinned:
		return string(metav1.ConditionUnknown), fmt.Sprintf("no supported version range is pinned for vendor %q", vendor)
	case SkewUnknownVendor:
		return string(metav1.ConditionFalse), fmt.Sprintf("vendor %q is not known; allowed values are %s",
			vendor, strings.Join(KnownVendors, ", "))
	default:
		if strings.TrimSpace(installed) == "" {
			return string(metav1.ConditionUnknown), fmt.Sprintf(
				"the installed %s operator version is unknown; set the %s annotation",
				vendor, VersionAnnotation)
		}
		return string(metav1.ConditionUnknown), fmt.Sprintf(
			"could not parse the installed %s operator version %q",
			vendor, installed)
	}
}

// ResolveVendor reads the enablement contract from a DpuOperatorConfig.
// spec.vendor wins; the annotation is the fallback. Invalid tokens
// (paths, URLs, injection) become "invalid" so KnownVendor rejects them
// without echoing the raw string.
func ResolveVendor(cfg *unstructured.Unstructured) string {
	if cfg == nil {
		return ""
	}
	vendor, _, _ := unstructured.NestedString(cfg.Object, vendorField...)
	if vendor == "" {
		vendor = cfg.GetAnnotations()[VendorAnnotation]
	}
	token, ok := SanitizeVendorToken(vendor)
	if !ok {
		return "invalid"
	}
	return token
}

// SetupWithManager wires the LCM to watch DpuOperatorConfig.
func (r *LifecycleManager) SetupWithManager(mgr ctrl.Manager) error {
	cfg := &unstructured.Unstructured{}
	cfg.SetGroupVersionKind(DpuOperatorConfigGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(cfg).
		Named("lifecycle-manager").
		Complete(r)
}
