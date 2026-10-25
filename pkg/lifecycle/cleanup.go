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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// CleanupFinalizer holds DpuOperatorConfig until cleanup has either
	// deleted adapter-owned resources or decided to preserve them.
	CleanupFinalizer = "lifecycle.opi.nvidia.com/cleanup"

	RemovalPolicyAnnotation  = "lifecycle.opi.nvidia.com/removal-policy"
	AdoptionPolicyAnnotation = "lifecycle.opi.nvidia.com/adoption-policy"
	ReleaseModeLabel         = "lifecycle.opi.nvidia.com/mode"
	helmStorageOwnerLabel    = "owner"

	OwnershipController = "controller"
	OwnershipAdopted    = "adopted"
	OwnershipExternal   = "external"
	OwnershipInvalid    = "invalid"

	ReleaseModeInstalled = "installed"
	ReleaseModeAdopted   = "adopted"

	RemovalRetain      = "retain"
	RemovalDeleteOwned = "delete-owned"

	CleanupDeleted   = "deleted"
	CleanupPreserved = "preserved"
	CleanupSkipped   = "skipped"
	CleanupFailed    = "failed"
	CleanupGone      = "already-gone"

	CleanupConditionType = "VendorOperatorCleanup"
)

// Cleanup errors. Callers use errors.Is. None of these authorize a delete.
var (
	ErrCleanupOwnership = errors.New("ownership is missing, conflicting, or not verified")
	ErrCleanupForbidden = errors.New("destructive cleanup is not allowed")
	ErrCleanupChanged   = errors.New("ownership changed before deletion")
)

// OwnershipClass is how a release relates to this adapter.
type OwnershipClass struct {
	Kind   string
	Owner  string
	Mode   string
	Reason string
}

// AllowsDelete is true only for verified adapter ownership. Policy is
// a separate check.
func (c OwnershipClass) AllowsDelete() bool {
	return c.Kind == OwnershipController || c.Kind == OwnershipAdopted
}

// ObjectRef is a namespaced identity. Name alone is never enough.
type ObjectRef struct {
	Group      string
	Version    string
	Kind       string
	Namespace  string
	Name       string
	Owner      string
	Tenant     string
	Referenced bool
}

func (o ObjectRef) Key() string {
	return o.Namespace + "/" + o.Kind + "/" + o.Name
}

func (o ObjectRef) GVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: o.Group, Version: o.Version, Kind: o.Kind}
}

// CleanupRefs is the dependency view used before any delete.
type CleanupRefs struct {
	OtherTenants  int
	OPI           []ObjectRef
	Dataplane     []ObjectRef
	CRDDependents int
}

// ResourceIndex lists tenants and shared objects. Tests inject a memory
// index so cleanup does not need live CRDs.
type ResourceIndex interface {
	Refs(ctx context.Context, exceptConfig, releaseNamespace string) (CleanupRefs, error)
	DeleteDataplane(ctx context.Context, ref ObjectRef) error
}

// MemoryResourceIndex is the in-process index used by cleanup tests.
type MemoryResourceIndex struct {
	Tenants        []string
	OPI            []ObjectRef
	Dataplane      []ObjectRef
	CRDDependents  int
	Deleted        []ObjectRef
	FlipReferenced bool
	looks          int
}

func (m *MemoryResourceIndex) Refs(_ context.Context, exceptConfig, releaseNamespace string) (CleanupRefs, error) {
	m.looks++
	out := CleanupRefs{CRDDependents: m.CRDDependents}
	for _, name := range m.Tenants {
		if name != "" && name != exceptConfig {
			out.OtherTenants++
		}
	}
	out.OPI = append([]ObjectRef{}, m.OPI...)
	for _, d := range m.Dataplane {
		if m.FlipReferenced && m.looks > 1 {
			d.Referenced = true
		}
		_ = releaseNamespace
		out.Dataplane = append(out.Dataplane, d)
	}
	return out, nil
}

func (m *MemoryResourceIndex) DeleteDataplane(_ context.Context, ref ObjectRef) error {
	kept := m.Dataplane[:0]
	found := false
	for _, d := range m.Dataplane {
		if d.Key() == ref.Key() {
			found = true
			m.Deleted = append(m.Deleted, d)
			continue
		}
		kept = append(kept, d)
	}
	m.Dataplane = kept
	if !found {
		return nil
	}
	return nil
}

// CleanupPlan is the decision. ExecuteCleanup revalidates before acting.
type CleanupPlan struct {
	Ownership       OwnershipClass
	Removal         string
	Adoption        string
	Action          string
	Reason          string
	DeleteRelease   bool
	DeleteCRDs      bool
	DeleteDataplane []ObjectRef
	Skipped         []string
}

// CleanupResult is what Reconcile records. Skipped and failed deletes
// are never reported as deleted.
type CleanupResult struct {
	Action       string
	Ownership    string
	Reason       string
	Release      string
	DeletedCRDs  bool
	DeletedPlane []string
	Skipped      []string
	Failure      string
	OpStatus     string
}

// ClassifyReleaseOwnership distinguishes controller-owned, adopted,
// external, and invalid metadata. Missing or conflicting labels are
// invalid: cleanup must preserve.
func ClassifyReleaseOwnership(rel HelmRelease) OwnershipClass {
	if rel.Name == "" {
		return OwnershipClass{Kind: OwnershipInvalid, Reason: "release identity is missing"}
	}
	labels := rel.Labels
	if labels == nil {
		return OwnershipClass{Kind: OwnershipInvalid, Reason: "ownership labels are missing"}
	}
	ours := strings.TrimSpace(labels[ReleaseOwnerLabel])
	helmOwner := strings.TrimSpace(labels[helmStorageOwnerLabel])
	mode := strings.TrimSpace(labels[ReleaseModeLabel])
	// Helm stores every release with owner=helm. That is storage, not an
	// external operator. A claimed pin keeps that label on purpose.
	if helmOwner == "helm" {
		helmOwner = ""
	}
	if strings.ContainsAny(ours, " \t,;") || strings.ContainsAny(mode, " \t,;") {
		return OwnershipClass{Kind: OwnershipInvalid, Owner: ours, Mode: mode,
			Reason: "ownership metadata is conflicting or malformed"}
	}
	if ours == "" && helmOwner == "" {
		if strings.TrimSpace(labels[helmStorageOwnerLabel]) == "helm" {
			return OwnershipClass{Kind: OwnershipExternal, Owner: "helm",
				Reason: "release is externally owned"}
		}
		return OwnershipClass{Kind: OwnershipInvalid, Reason: "ownership labels are missing"}
	}
	if ours != "" && helmOwner != "" && ours != helmOwner {
		return OwnershipClass{
			Kind: OwnershipInvalid, Owner: ours, Mode: mode,
			Reason: "ownership labels conflict",
		}
	}
	if ours != "" && ours != ReleaseOwner {
		return OwnershipClass{Kind: OwnershipExternal, Owner: ours, Mode: mode,
			Reason: "release is externally owned"}
	}
	if ours == "" {
		return OwnershipClass{Kind: OwnershipExternal, Owner: helmOwner,
			Reason: "release is externally owned"}
	}
	if mode == ReleaseModeInstalled {
		return OwnershipClass{Kind: OwnershipController, Owner: ours, Mode: mode,
			Reason: "controller-owned release"}
	}
	return OwnershipClass{Kind: OwnershipAdopted, Owner: ours, Mode: mode,
		Reason: "adopted release"}
}

func parsePolicy(raw, defaultValue string) (string, string) {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return defaultValue, ""
	case RemovalRetain, RemovalDeleteOwned:
		return v, ""
	default:
		return RemovalRetain, "invalid removal policy; defaulting to retain"
	}
}

func policiesFrom(cfg *unstructured.Unstructured) (removal, adoption, note string) {
	ann := map[string]string{}
	if cfg != nil {
		ann = cfg.GetAnnotations()
	}
	var extra string
	removal, extra = parsePolicy(ann[RemovalPolicyAnnotation], RemovalRetain)
	adoption, note = parsePolicy(ann[AdoptionPolicyAnnotation], RemovalRetain)
	if extra != "" {
		note = extra
	}
	return removal, adoption, note
}

func referencedByOPI(d ObjectRef, opi []ObjectRef) bool {
	if d.Referenced {
		return true
	}
	for _, o := range opi {
		if o.Name == d.Name && (o.Namespace == "" || o.Namespace == d.Namespace) {
			return true
		}
		if o.Name != "" && strings.EqualFold(o.Kind, d.Kind) && o.Namespace == d.Namespace && o.Name == d.Name {
			return true
		}
	}
	return false
}

// DecideCleanup is the non-destructive decision. Nothing is deleted here.
func DecideCleanup(rel HelmRelease, removal, adoption string, refs CleanupRefs, allowCRD bool) CleanupPlan {
	plan := CleanupPlan{Removal: removal, Adoption: adoption}
	if rel.Name == "" {
		plan.Ownership = OwnershipClass{Kind: OwnershipInvalid, Reason: "release is absent"}
		plan.Action = CleanupGone
		plan.Reason = alreadyGoneReason
		return plan
	}
	class := ClassifyReleaseOwnership(rel)
	plan.Ownership = class
	if class.Kind == OwnershipInvalid {
		plan.Action = CleanupFailed
		plan.Reason = class.Reason
		plan.Skipped = append(plan.Skipped, "release: "+class.Reason)
		return plan
	}
	if class.Kind == OwnershipExternal {
		plan.Action = CleanupPreserved
		plan.Reason = class.Reason
		plan.Skipped = append(plan.Skipped, "release: externally owned")
		return plan
	}

	allow := removal == RemovalDeleteOwned
	if class.Kind == OwnershipAdopted {
		allow = adoption == RemovalDeleteOwned
	}
	if !allow {
		plan.Action = CleanupPreserved
		plan.Reason = "removal policy retains the release"
		plan.Skipped = append(plan.Skipped, "release: policy is retain")
		return plan
	}

	plan.DeleteRelease = true
	shared := refs.OtherTenants > 0 || refs.CRDDependents > 0 || len(refs.OPI) > 0
	if allowCRD && !shared && !dataplaneStillRequired(refs, rel.Namespace) {
		plan.DeleteCRDs = true
	} else {
		plan.Skipped = append(plan.Skipped, "crds: shared or still required")
	}

	for _, d := range refs.Dataplane {
		if d.Namespace != rel.Namespace {
			plan.Skipped = append(plan.Skipped,
				"dataplane "+d.Key()+": different namespace")
			continue
		}
		if d.Owner != "" && d.Owner != ReleaseOwner {
			plan.Skipped = append(plan.Skipped, "dataplane "+d.Key()+": not owned")
			continue
		}
		if referencedByOPI(d, refs.OPI) {
			plan.Skipped = append(plan.Skipped,
				"dataplane "+d.Key()+": still referenced")
			continue
		}
		plan.DeleteDataplane = append(plan.DeleteDataplane, d)
	}
	plan.Action = CleanupDeleted
	plan.Reason = "verified ownership and delete-owned policy"
	return plan
}

func dataplaneStillRequired(refs CleanupRefs, ns string) bool {
	for _, d := range refs.Dataplane {
		if d.Namespace == ns && (d.Referenced || referencedByOPI(d, refs.OPI)) {
			return true
		}
	}
	return false
}

func (r *LifecycleManager) resourceIndex() ResourceIndex {
	if r.Index != nil {
		return r.Index
	}
	if r.Client != nil {
		return ClientResourceIndex{Client: r.Client}
	}
	return &MemoryResourceIndex{}
}

func (r *LifecycleManager) allowCRDDeletion() bool {
	return r.AllowCRDDeletion
}

// CleanupInstallations is the guarded cleanup entry. It re-reads the
// release immediately before any delete. Retries classify again.
func (r *LifecycleManager) CleanupInstallations(
	ctx context.Context, cfg *unstructured.Unstructured,
) CleanupResult {
	log := logf.FromContext(ctx).WithName("lifecycle-cleanup")
	removal, adoption, note := policiesFrom(cfg)
	except := ""
	if cfg != nil {
		except = cfg.GetName()
	}
	out := CleanupResult{Action: CleanupPreserved, OpStatus: OpSucceeded}

	if r.Helm == nil {
		out.Action = CleanupFailed
		out.Ownership = OwnershipInvalid
		out.Reason = "ownership cannot be verified without a helm client"
		out.Failure = ErrCleanupOwnership.Error()
		out.OpStatus = OpFailed
		out.Skipped = []string{"release: helm client is not configured"}
		log.Info("cleanup skipped", "reason", out.Reason)
		return out
	}

	install, err := DefaultInstallConfig(VendorNVIDIA)
	if err != nil {
		out.Action = CleanupFailed
		out.Failure = err.Error()
		out.OpStatus = OpFailed
		return out
	}
	key := releaseKey(install.Namespace, install.ReleaseName)
	unlock := r.cleanupLocks().acquire(key)
	defer unlock()

	rel, getErr := r.Helm.GetRelease(ctx, install.Namespace, install.ReleaseName)
	if errors.Is(getErr, ErrReleaseNotFound) {
		out.Action = CleanupGone
		out.Reason = alreadyGoneReason
		out.Release = key
		log.Info("cleanup idempotent", "reason", out.Reason, "release", key)
		return out
	}
	if getErr != nil {
		out.Action = CleanupFailed
		out.Failure = getErr.Error()
		out.Reason = "could not read the release"
		out.OpStatus = OpFailed
		return out
	}

	idx := r.resourceIndex()
	refs, refErr := idx.Refs(ctx, except, install.Namespace)
	if refErr != nil {
		out.Action = CleanupFailed
		out.Ownership = ClassifyReleaseOwnership(rel).Kind
		out.Failure = refErr.Error()
		out.Reason = "could not list dependents; preserving"
		out.OpStatus = OpFailed
		out.Skipped = []string{"dependents: lookup failed"}
		log.Info("cleanup skipped", "reason", out.Reason)
		return out
	}

	plan := DecideCleanup(rel, removal, adoption, refs, r.allowCRDDeletion())
	if note != "" {
		plan.Skipped = append(plan.Skipped, note)
	}
	out.Ownership = plan.Ownership.Kind
	out.Reason = plan.Reason
	out.Release = rel.Namespace + "/" + rel.Name
	out.Skipped = plan.Skipped

	if !plan.DeleteRelease {
		if plan.Action == CleanupFailed {
			out.Action = CleanupFailed
			out.Failure = ErrCleanupOwnership.Error()
			out.OpStatus = OpFailed
		} else {
			out.Action = plan.Action
		}
		log.Info("cleanup skipped",
			"reason", SanitizeLogValue("reason", plan.Reason),
			"ownership", plan.Ownership.Kind,
			"release", out.Release)
		return out
	}

	actual, err := r.Helm.GetRelease(ctx, install.Namespace, install.ReleaseName)
	if errors.Is(err, ErrReleaseNotFound) {
		out.Action = CleanupGone
		out.Reason = "release disappeared before deletion"
		return out
	}
	if err != nil {
		out.Action = CleanupFailed
		out.Failure = err.Error()
		out.OpStatus = OpFailed
		return out
	}
	again := ClassifyReleaseOwnership(actual)
	if again.Kind != plan.Ownership.Kind || !again.AllowsDelete() {
		out.Action = CleanupFailed
		out.Ownership = again.Kind
		out.Reason = "ownership changed before deletion; preserving"
		out.Failure = ErrCleanupChanged.Error()
		out.OpStatus = OpFailed
		out.Skipped = append(out.Skipped, "release: ownership changed")
		log.Info("cleanup skipped", "reason", out.Reason, "ownership", again.Kind)
		return out
	}

	if _, err := r.Helm.Uninstall(ctx, install); err != nil {
		if errors.Is(err, ErrReleaseNotFound) {
			out.Action = CleanupGone
			out.Reason = alreadyGoneReason
			return out
		}
		out.Action = CleanupFailed
		out.Failure = err.Error()
		out.Reason = "uninstall failed; release preserved"
		out.OpStatus = OpFailed
		log.Info("cleanup failed", "reason", out.Reason)
		return out
	}

	deleted := []string{}
	liveRefs, _ := idx.Refs(ctx, except, install.Namespace)
	for _, d := range plan.DeleteDataplane {
		if stillProtected(d, liveRefs) {
			out.Skipped = append(out.Skipped, "dataplane "+d.Key()+": reference appeared")
			continue
		}
		if err := idx.DeleteDataplane(ctx, d); err != nil {
			out.Skipped = append(out.Skipped, "dataplane "+d.Key()+": "+err.Error())
			continue
		}
		deleted = append(deleted, d.Key())
	}

	out.Action = CleanupDeleted
	out.DeletedCRDs = plan.DeleteCRDs
	out.DeletedPlane = deleted
	out.OpStatus = OpSucceeded
	log.Info("cleanup deleted adapter-owned release",
		"release", out.Release, "crds", plan.DeleteCRDs, "dataplane", len(deleted))
	return out
}

func stillProtected(d ObjectRef, refs CleanupRefs) bool {
	for _, cur := range refs.Dataplane {
		if cur.Key() == d.Key() && referencedByOPI(cur, refs.OPI) {
			return true
		}
	}
	return referencedByOPI(d, refs.OPI)
}

func (r *LifecycleManager) cleanupLocks() *ReleaseLocks {
	if r.Locks != nil {
		return r.Locks
	}
	return defaultReleaseLocks
}

// ClientResourceIndex lists live OPI and DPF objects. Missing CRDs
// are treated as zero dependents, not as permission to delete.
type ClientResourceIndex struct {
	Client client.Client
}

func (i ClientResourceIndex) Refs(
	ctx context.Context, exceptConfig, releaseNamespace string,
) (CleanupRefs, error) {
	out := CleanupRefs{}
	if i.Client == nil {
		return out, nil
	}
	cfgs := &unstructured.UnstructuredList{}
	cfgs.SetGroupVersionKind(DpuOperatorConfigGVK.GroupVersion().WithKind("DpuOperatorConfigList"))
	if err := i.Client.List(ctx, cfgs); err != nil && !meta.IsNoMatchError(err) {
		return out, err
	} else if err == nil {
		for j := range cfgs.Items {
			item := &cfgs.Items[j]
			if item.GetName() == exceptConfig || item.GetDeletionTimestamp() != nil {
				continue
			}
			if v := ResolveVendor(item); v == VendorNVIDIA || v == VendorAMD {
				out.OtherTenants++
			}
		}
	}

	for _, gvk := range []schema.GroupVersionKind{
		{Group: opiConfigGroup, Version: "v1", Kind: "DataProcessingUnit"},
		{Group: opiConfigGroup, Version: "v1", Kind: "ServiceFunctionChain"},
	} {
		items, err := listGVK(ctx, i.Client, gvk, "")
		if err != nil {
			continue
		}
		out.OPI = append(out.OPI, items...)
		out.CRDDependents += len(items)
	}
	for _, gvk := range requiredDPFCRDs {
		items, err := listGVK(ctx, i.Client, gvk, releaseNamespace)
		if err != nil {
			continue
		}
		out.Dataplane = append(out.Dataplane, items...)
		out.CRDDependents += len(items)
	}
	return out, nil
}

func (i ClientResourceIndex) DeleteDataplane(ctx context.Context, ref ObjectRef) error {
	if i.Client == nil {
		return fmt.Errorf("no cluster client")
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ref.GVK())
	key := client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}
	if err := i.Client.Get(ctx, key, obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if obj.GetNamespace() != ref.Namespace || obj.GetName() != ref.Name {
		return fmt.Errorf("namespace or name mismatch")
	}
	return client.IgnoreNotFound(i.Client.Delete(ctx, obj))
}

func listGVK(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, ns string) ([]ObjectRef, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	opts := []client.ListOption{}
	if ns != "" {
		opts = append(opts, client.InNamespace(ns))
	}
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}
	out := make([]ObjectRef, 0, len(list.Items))
	for i := range list.Items {
		item := &list.Items[i]
		owner := item.GetLabels()[ReleaseOwnerLabel]
		out = append(out, ObjectRef{
			Group:      gvk.Group,
			Version:    gvk.Version,
			Kind:       gvk.Kind,
			Namespace:  item.GetNamespace(),
			Name:       item.GetName(),
			Owner:      owner,
			Referenced: item.GetAnnotations()["translation.opi.nvidia.com/source"] != "",
		})
	}
	return out, nil
}
