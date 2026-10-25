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
// The LCM watches DpuOperatorConfig and runs EnsureInstalled: intent, preflight,
// version policy, installer, readiness. The default installer detects an
// existing vendor operator. It does not helm-install DPF.
package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DpuOperatorConfigGVK is the cluster-scoped config the LCM reconciles.
var DpuOperatorConfigGVK = schema.GroupVersionKind{
	Group:   opiConfigGroup,
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

// VersionAnnotation is the operator's version pin. It beats Helm chart
// and OLM CSV auto-detection so a human pin stays authoritative.
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

// LifecycleManager installs and pins the requested vendor's operator.
// Reconcile calls EnsureInstalled; it does not stop at a log line.
type LifecycleManager struct {
	client.Client
	Scheme *runtime.Scheme
	// Now supplies the clock for condition timestamps. Nil means time.Now.
	// It is injectable because lastTransitionTime is formatted to RFC3339,
	// which has second precision: two reconciles in the same second produce
	// an identical string, so without a controllable clock a test cannot tell
	// a preserved timestamp from a rewritten one.
	Now func() time.Time
	// Installer is the vendor-operator ensure step. Nil falls through to
	// HelmInstaller when Helm is set, otherwise DetectingInstaller.
	Installer Installer
	// Helm is the dedicated Helm SDK. When set (and Installer is nil),
	// EnsureInstalled installs or adopts the pinned release. It never
	// adopts a release this adapter does not own.
	Helm HelmClient
	// OLM observes operators.coreos.com Subscriptions and CSVs. It
	// never creates one. Helm storage, when present, wins.
	OLM OLMClient
	// Probe is the cluster-access / dependency view. Nil builds a
	// RESTClusterProbe from this client and Discovery.
	Probe ClusterProbe
	// Discovery supplies the Kubernetes version. Required in production so
	// a missing apiserver cannot be reported as Supported.
	Discovery discovery.ServerVersionInterface
	// Index lists tenants and shared objects for cleanup. Nil uses the
	// Kubernetes client or an empty memory index.
	Index ResourceIndex
	// Locks serialize upgrade, rollback, and cleanup per release.
	Locks *ReleaseLocks
	// AllowCRDDeletion is false in production. Shared DPF CRDs stay.
	AllowCRDDeletion bool
}

func (r *LifecycleManager) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// LCM lists these to decide Kamaji preflight and to refuse cleanup of
// shared operator objects. No create/update/delete: this adapter does
// not own DPF CRDs or the live Helm release.
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// ObserveNVIDIADPUs lists DPU objects. No create/update/delete: reboot
// is host-side (mlxfwreset) and is not run from this adapter.
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
// Helm storage (sh.helm.release.v1.*) is read to classify unmanaged vs owned.
// No create/delete: claim may patch owner labels only.
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;update;patch
// OLM Subscription/CSV are observed when operators.coreos.com exists.
// No create/delete: DPF is not an OperatorHub package on this pin.
// +kubebuilder:rbac:groups=operators.coreos.com,resources=subscriptions,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list

func (r *LifecycleManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("lifecycle")
	unlock := r.configLocks().acquire(configLockKey(req.NamespacedName))
	defer unlock()

	cfg := &unstructured.Unstructured{}
	cfg.SetGroupVersionKind(DpuOperatorConfigGVK)
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if ts := cfg.GetDeletionTimestamp(); ts != nil && !ts.IsZero() {
		return r.reconcileCleanup(ctx, cfg)
	}

	if err := r.ensureCleanupFinalizer(ctx, req.NamespacedName, cfg); err != nil {
		return ctrl.Result{}, err
	}

	out, err := r.EnsureInstalled(ctx, cfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if out.Preflight.Skip {
		log.Info("DpuOperatorConfig has no requested vendor; nothing to install",
			"config", req.NamespacedName,
			"hint", "set spec.vendor or the "+VendorAnnotation+" annotation to nvidia or amd")
		return ctrl.Result{}, nil
	}

	log.Info("ensure installed",
		vendorKey, out.Intent.Vendor, //nolint:logcheck // vendorKey is the enablement field name
		"source", string(out.Intent.Source),
		"action", out.Install.Action,
		"installed", out.Policy.Version,
		"skew", string(out.Policy.Skew),
		"ready", out.Readiness.Ready,
		"operator", out.Operator.Phase,
		"kubernetes", out.Dependencies.Kubernetes,
		"missing", strings.Join(out.Dependencies.Missing, ","),
		"config", req.NamespacedName)

	if err := r.setOwnedConditions(ctx, cfg, out); err != nil {
		return ctrl.Result{}, err
	}
	if !out.Readiness.Ready || dpuNeedsRequeue(out.DPU) {
		return ctrl.Result{RequeueAfter: DPURequeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

func (r *LifecycleManager) reconcileCleanup(ctx context.Context, cfg *unstructured.Unstructured) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("lifecycle")
	if !controllerutil.ContainsFinalizer(cfg, CleanupFinalizer) {
		return ctrl.Result{}, nil
	}

	got := r.CleanupInstallations(ctx, cfg)
	log.Info("cleanup finished",
		"action", got.Action,
		"ownership", got.Ownership,
		"reason", SanitizeLogValue("reason", got.Reason),
		"release", got.Release,
		"skipped", strings.Join(got.Skipped, ","))

	if err := r.upsertConditions(ctx, cfg, []ownedCondition{{
		Type:    CleanupConditionType,
		Status:  cleanupConditionStatus(got),
		Reason:  got.Action,
		Message: cleanupConditionMessage(got),
	}}); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if got.OpStatus == OpFailed && got.Action == CleanupFailed &&
		got.Failure == ErrCleanupChanged.Error() {
		return ctrl.Result{}, fmt.Errorf("%w", ErrCleanupChanged)
	}

	if err := r.removeCleanupFinalizer(ctx, client.ObjectKeyFromObject(cfg)); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func cleanupConditionStatus(got CleanupResult) string {
	if got.OpStatus == OpFailed {
		return string(metav1.ConditionFalse)
	}
	return string(metav1.ConditionTrue)
}

func cleanupConditionMessage(got CleanupResult) string {
	msg := got.Reason
	if got.Failure != "" {
		msg = got.Reason + ": " + got.Failure
	}
	if len(got.Skipped) > 0 {
		msg += "; skipped " + strings.Join(got.Skipped, "; ")
	}
	return SanitizeLogValue("message", msg)
}

// setSkewCondition upserts the LCM's own condition by type, preserving
// lastTransitionTime while the status is unchanged so the field keeps the
// meaning metav1 gives it.
type ownedCondition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

func (r *LifecycleManager) setOwnedConditions(ctx context.Context, cfg *unstructured.Unstructured, out Result) error {
	skew := out.Policy.Skew
	status, message := skewStatus(out.Intent.Vendor, out.Policy.Version, skew)
	if out.Policy.Message != "" {
		message = out.Policy.Message
	}
	readyStatus, readyReason, readyMsg := operatorCondition(out.Operator)
	dpuStatus, dpuReason, dpuMsg := dpuCondition(out.DPU)
	return r.upsertConditions(ctx, cfg, []ownedCondition{
		{Type: SupportedConditionType, Status: status, Reason: string(skew), Message: message},
		{Type: OperatorReadyConditionType, Status: readyStatus, Reason: readyReason, Message: readyMsg},
		{Type: DPUReadyConditionType, Status: dpuStatus, Reason: dpuReason, Message: dpuMsg},
	})
}

func (r *LifecycleManager) upsertConditions(
	ctx context.Context, cfg *unstructured.Unstructured, want []ownedCondition,
) error {
	if r.Client == nil {
		_, err := r.mergeOwnedConditions(cfg, want)
		return err
	}
	key := client.ObjectKeyFromObject(cfg)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(DpuOperatorConfigGVK)
		if err := r.Get(ctx, key, fresh); err != nil {
			return err
		}
		changed, err := r.mergeOwnedConditions(fresh, want)
		if err != nil {
			return err
		}
		if !changed {
			cfg.Object = fresh.Object
			return nil
		}
		if err := r.Status().Update(ctx, fresh); err != nil {
			return err
		}
		cfg.Object = fresh.Object
		return nil
	})
}

func (r *LifecycleManager) mergeOwnedConditions(cfg *unstructured.Unstructured, want []ownedCondition) (bool, error) {
	if raw, ok := cfg.Object[statusKey].(map[string]any); !ok || raw == nil {
		if err := unstructured.SetNestedMap(cfg.Object, map[string]any{}, statusKey); err != nil {
			return false, fmt.Errorf("initializing status: %w", err)
		}
	}
	conds, _, err := unstructured.NestedSlice(cfg.Object, statusKey, "conditions")
	if err != nil {
		return false, fmt.Errorf("reading status.conditions: %w", err)
	}
	changed := false
	for _, next := range want {
		item := map[string]any{
			"type":               next.Type,
			statusKey:            next.Status,
			"reason":             next.Reason,
			"message":            next.Message,
			"lastTransitionTime": r.now().UTC().Format(time.RFC3339),
		}
		replaced := false
		for i, raw := range conds {
			c, ok := raw.(map[string]any)
			if !ok || c["type"] != next.Type {
				continue
			}
			if c[statusKey] == next.Status && c["reason"] == next.Reason && c["message"] == next.Message {
				replaced = true
				break
			}
			if c[statusKey] == next.Status {
				item["lastTransitionTime"] = c["lastTransitionTime"]
			}
			conds[i] = item
			replaced = true
			changed = true
			break
		}
		if !replaced {
			conds = append(conds, item)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if err := unstructured.SetNestedSlice(cfg.Object, conds, statusKey, "conditions"); err != nil {
		return false, fmt.Errorf("setting status.conditions: %w", err)
	}
	return true, nil
}

func (r *LifecycleManager) configLocks() *ReleaseLocks {
	if r != nil && r.Locks != nil {
		return r.Locks
	}
	return defaultReleaseLocks
}

func configLockKey(nn types.NamespacedName) string {
	return "dpuoperatorconfig/" + nn.Namespace + "/" + nn.Name
}

func (r *LifecycleManager) ensureCleanupFinalizer(ctx context.Context, nn types.NamespacedName, cfg *unstructured.Unstructured) error {
	if !KnownVendor(ResolveVendor(cfg)) {
		return nil
	}
	if controllerutil.ContainsFinalizer(cfg, CleanupFinalizer) {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(DpuOperatorConfigGVK)
		if err := r.Get(ctx, nn, fresh); err != nil {
			return err
		}
		if !KnownVendor(ResolveVendor(fresh)) || controllerutil.ContainsFinalizer(fresh, CleanupFinalizer) {
			cfg.Object = fresh.Object
			return nil
		}
		controllerutil.AddFinalizer(fresh, CleanupFinalizer)
		if err := r.Update(ctx, fresh); err != nil {
			return err
		}
		cfg.Object = fresh.Object
		return nil
	})
}

func (r *LifecycleManager) removeCleanupFinalizer(ctx context.Context, nn types.NamespacedName) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(DpuOperatorConfigGVK)
		if err := r.Get(ctx, nn, fresh); err != nil {
			return err
		}
		if !controllerutil.ContainsFinalizer(fresh, CleanupFinalizer) {
			return nil
		}
		controllerutil.RemoveFinalizer(fresh, CleanupFinalizer)
		return r.Update(ctx, fresh)
	})
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
	case SkewInvalidConfig:
		return string(metav1.ConditionFalse),
			"install settings are not the pinned vendor/version/namespace; " +
				"chart URLs and helm arguments are rejected"
	case SkewBlocked:
		return string(metav1.ConditionFalse),
			"cluster access, Kubernetes version, or a DPF prerequisite is missing"
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

// SetupWithManager wires the LCM to watch DpuOperatorConfig and NVIDIA DPU
// objects. A DPU phase change (Rebooting → Ready) must not wait on the 5s
// requeue timer alone.
func (r *LifecycleManager) SetupWithManager(mgr ctrl.Manager) error {
	cfg := &unstructured.Unstructured{}
	cfg.SetGroupVersionKind(DpuOperatorConfigGVK)
	dpu := &unstructured.Unstructured{}
	dpu.SetGroupVersionKind(nvidiaProbeGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(cfg).
		Watches(dpu, handler.EnqueueRequestsFromMapFunc(r.enqueueDpuOperatorConfigs)).
		Named("lifecycle-manager").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

func (r *LifecycleManager) enqueueDpuOperatorConfigs(ctx context.Context, _ client.Object) []reconcile.Request {
	if r == nil || r.Client == nil {
		return nil
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(DpuOperatorConfigGVK.GroupVersion().WithKind(DpuOperatorConfigGVK.Kind + "List"))
	if err := r.List(ctx, list); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      list.Items[i].GetName(),
			Namespace: list.Items[i].GetNamespace(),
		}})
	}
	return reqs
}
