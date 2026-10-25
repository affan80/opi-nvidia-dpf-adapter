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
	"maps"
	"strings"
	"sync"
	"time"
)

// ReleaseOwnerLabel is stamped on every release this adapter installs.
// Adopt only when this label equals ReleaseOwner.
const ReleaseOwnerLabel = "lifecycle.opi.nvidia.com/owner"

// HelmStatusDeployed is the Helm storage status Install and Upgrade write.
const HelmStatusDeployed = "deployed"

const redactedLogValue = "[redacted]"

const defaultReleaseAttempts = 2

// Operation status recorded on InstallResult. A timeout or rollback
// failure is never reported as succeeded.
const (
	OpSucceeded      = "succeeded"
	OpFailed         = "failed"
	OpTimeout        = "timeout"
	OpUnknown        = "unknown"
	OpRolledBack     = "rolled_back"
	OpRollbackFailed = "rollback_failed"
	OpNoop           = "noop"
)

// Helm storage / action errors. Callers use errors.Is.
var (
	ErrReleaseNotFound  = errors.New("helm release not found")
	ErrReleaseExists    = errors.New("helm release already exists")
	ErrReleaseUnmanaged = errors.New("helm release is not managed by this adapter")
	ErrReleaseTimeout   = errors.New("helm release operation timed out")
	ErrReleaseUnknown   = errors.New("helm release operation outcome is unknown")
	ErrRollbackFailed   = errors.New("rollback failed")
	// ErrNoVendorChart means this vendor has no published Helm/OLM
	// package. The installer chain falls through to DetectingInstaller
	// instead of treating skip as a finished AMD install.
	ErrNoVendorChart = errors.New("vendor has no pinned helm chart")
)

// HelmRelease is the one identity fresh-install and adopt must share:
// namespace/name @ chartVersion, owned by this adapter.
type HelmRelease struct {
	Name          string
	Namespace     string
	ChartName     string
	ChartVersion  string
	Status        string
	Revision      int
	LastKnownGood string
	Labels        map[string]string
}

// Identity is namespace/name@chartVersion. Tests use this to prove both
// paths converge on one release.
func (r HelmRelease) Identity() string {
	return r.Namespace + "/" + r.Name + "@" + r.ChartVersion
}

// ManagedByAdapter is the adopt gate. A Helm release that already exists
// but lacks this owner is left alone (unmanaged).
func (r HelmRelease) ManagedByAdapter() bool {
	if r.Labels == nil {
		return false
	}
	return r.Labels[ReleaseOwnerLabel] == ReleaseOwner
}

// HelmClient is the dedicated Helm SDK surface. No helm CLI, no user
// --set, no chart URL outside InstallConfig.
type HelmClient interface {
	GetRelease(ctx context.Context, namespace, name string) (HelmRelease, error)
	Install(ctx context.Context, cfg InstallConfig, values map[string]any) (HelmRelease, error)
	Upgrade(ctx context.Context, cfg InstallConfig, values map[string]any) (HelmRelease, error)
	Rollback(ctx context.Context, cfg InstallConfig, toVersion string) (HelmRelease, error)
	Uninstall(ctx context.Context, cfg InstallConfig) (HelmRelease, error)
}

// lastGoodStore is implemented by MemoryHelmClient. A live client can
// omit it; HelmInstaller then only sees LastKnownGood on the release.
type lastGoodStore interface {
	RememberLastKnownGood(namespace, name string, rel HelmRelease)
	LastKnownGood(namespace, name string) (HelmRelease, bool)
}

// SafeHelmValues is the only values map the installer may pass. Kamaji
// and extra monitoring stay off unless a later task opts in. HelmArgs
// stay empty because ValidateInstallConfig rejects them.
func SafeHelmValues(cfg InstallConfig) map[string]any {
	if err := ValidateInstallConfig(cfg); err != nil {
		return nil
	}
	if cfg.Vendor != VendorNVIDIA {
		return map[string]any{}
	}
	return map[string]any{
		"kamajiClusterManager": map[string]any{"disable": true},
		"monitoring":           map[string]any{"disable": true},
	}
}

// OwnedRelease is the release Install and Upgrade both write.
func OwnedRelease(cfg InstallConfig) HelmRelease {
	return HelmRelease{
		Name:         cfg.ReleaseName,
		Namespace:    cfg.Namespace,
		ChartName:    cfg.ChartName,
		ChartVersion: cfg.ChartVersion,
		Status:       HelmStatusDeployed,
		Revision:     1,
		Labels: map[string]string{
			ReleaseOwnerLabel: cfg.ReleaseOwner,
			ReleaseModeLabel:  ReleaseModeInstalled,
		},
	}
}

func releaseKey(namespace, name string) string {
	return namespace + "/" + name
}

func requirePinnedConfig(cfg InstallConfig) error {
	if err := ValidateArtifactRef(cfg); err != nil {
		return err
	}
	if cfg.Vendor == VendorAMD {
		return fmt.Errorf("amd has no pinned helm chart")
	}
	return nil
}

// SanitizeLogValue redacts tokens, passwords, and kubeconfig-like
// strings so Helm values never land in logs.
func SanitizeLogValue(key, value string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, s := range []string{"password", "token", "secret", "credential", "kubeconfig", "authorization"} {
		if strings.Contains(k, s) {
			return redactedLogValue
		}
	}
	v := strings.TrimSpace(value)
	if strings.Contains(strings.ToLower(v), "bearer ") ||
		strings.Contains(v, "BEGIN ") ||
		strings.Contains(strings.ToLower(v), "kubeconfig") {
		return redactedLogValue
	}
	return value
}

// ReleaseLocks serializes upgrade and rollback for one namespace/name.
type ReleaseLocks struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func newReleaseLocks() *ReleaseLocks {
	return &ReleaseLocks{m: map[string]*sync.Mutex{}}
}

func (l *ReleaseLocks) acquire(key string) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	if l.m == nil {
		l.m = map[string]*sync.Mutex{}
	}
	lk, ok := l.m[key]
	if !ok {
		lk = &sync.Mutex{}
		l.m[key] = lk
	}
	l.mu.Unlock()
	lk.Lock()
	return lk.Unlock
}

var defaultReleaseLocks = newReleaseLocks()

// MemoryHelmClient is the in-process Helm SDK used by tests. Fresh
// install and adopt share the same map key: namespace/name.
type MemoryHelmClient struct {
	mu            sync.Mutex
	rels          map[string]HelmRelease
	history       map[string][]HelmRelease
	lastGood      map[string]HelmRelease
	inflight      map[string]int
	MaxInflight   int
	Upgrades      int
	Uninstalls    int
	Rollbacks     int
	Hold          time.Duration
	UpgradeErr    error
	FailNUpgrades int
	failed        int
	InstallErr    error
	FailNInstalls int
	installFailed int
	UninstallErr  error
	Timeout       bool
	Unknown       bool
	RollbackErr   error
}

// GetRelease returns the stored release or ErrReleaseNotFound.
func (m *MemoryHelmClient) GetRelease(_ context.Context, namespace, name string) (HelmRelease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rels == nil {
		return HelmRelease{}, ErrReleaseNotFound
	}
	rel, ok := m.rels[releaseKey(namespace, name)]
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	return rel, nil
}

func (m *MemoryHelmClient) begin(key string) {
	if m.inflight == nil {
		m.inflight = map[string]int{}
	}
	m.inflight[key]++
	if m.inflight[key] > m.MaxInflight {
		m.MaxInflight = m.inflight[key]
	}
}

func (m *MemoryHelmClient) end(key string) {
	if m.inflight == nil {
		return
	}
	m.inflight[key]--
}

// Install writes the pinned owned release. It refuses a second name in
// the same namespace.
func (m *MemoryHelmClient) Install(_ context.Context, cfg InstallConfig, _ map[string]any) (HelmRelease, error) {
	if err := requirePinnedConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rels == nil {
		m.rels = map[string]HelmRelease{}
	}
	if _, ok := m.rels[key]; ok {
		return HelmRelease{}, ErrReleaseExists
	}
	if m.InstallErr != nil && (m.FailNInstalls == 0 || m.installFailed < m.FailNInstalls) {
		m.installFailed++
		return HelmRelease{}, m.InstallErr
	}
	rel := OwnedRelease(cfg)
	m.rels[key] = rel
	m.appendHistory(key, rel)
	return rel, nil
}

// Upgrade replaces an owned release with the pinned identity. It will
// not take over an unmanaged release. Last-known-good is saved first.
func (m *MemoryHelmClient) Upgrade(ctx context.Context, cfg InstallConfig, _ map[string]any) (HelmRelease, error) {
	if err := requirePinnedConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	m.begin(key)
	m.Upgrades++
	hold := m.Hold
	m.mu.Unlock()
	if hold > 0 {
		time.Sleep(hold)
	}
	m.mu.Lock()
	defer func() {
		m.end(key)
		m.mu.Unlock()
	}()
	existing, ok := m.rels[key]
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	if !existing.ManagedByAdapter() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	if m.lastGood == nil {
		m.lastGood = map[string]HelmRelease{}
	}
	if _, ok := m.lastGood[key]; !ok {
		m.lastGood[key] = existing
	}
	if err := ctx.Err(); err != nil {
		return existing, err
	}
	if m.Timeout {
		return existing, ErrReleaseTimeout
	}
	if m.Unknown {
		return existing, ErrReleaseUnknown
	}
	if m.UpgradeErr != nil && (m.FailNUpgrades == 0 || m.failed < m.FailNUpgrades) {
		m.failed++
		return existing, m.UpgradeErr
	}
	rel := OwnedRelease(cfg)
	rel.Revision = existing.Revision + 1
	rel.LastKnownGood = existing.ChartVersion
	m.rels[key] = rel
	m.appendHistory(key, rel)
	return rel, nil
}

// Rollback restores the last-known-good revision. Failure does not
// rewrite the current release to look successful.
func (m *MemoryHelmClient) Rollback(_ context.Context, cfg InstallConfig, toVersion string) (HelmRelease, error) {
	if err := requirePinnedConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	if err := ValidateReleaseVersion(toVersion); err != nil {
		return HelmRelease{}, err
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rollbacks++
	existing, ok := m.rels[key]
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	if !existing.ManagedByAdapter() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	if m.RollbackErr != nil {
		return existing, ErrRollbackFailed
	}
	prev, ok := m.lastGood[key]
	if !ok || !versionsEqual(prev.ChartVersion, toVersion) {
		return existing, ErrRollbackFailed
	}
	prev.Revision = existing.Revision + 1
	prev.LastKnownGood = prev.ChartVersion
	m.rels[key] = prev
	m.appendHistory(key, prev)
	return prev, nil
}

// Uninstall removes one pinned adapter-owned release. External or
// conflicting ownership is refused. The key is namespace/name.
func (m *MemoryHelmClient) Uninstall(_ context.Context, cfg InstallConfig) (HelmRelease, error) {
	if err := requirePinnedConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Uninstalls++
	existing, ok := m.rels[key]
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	if !ClassifyReleaseOwnership(existing).AllowsDelete() {
		return existing, ErrReleaseUnmanaged
	}
	if m.UninstallErr != nil {
		return existing, m.UninstallErr
	}
	delete(m.rels, key)
	return existing, nil
}

func (m *MemoryHelmClient) RememberLastKnownGood(namespace, name string, rel HelmRelease) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastGood == nil {
		m.lastGood = map[string]HelmRelease{}
	}
	m.lastGood[releaseKey(namespace, name)] = rel
}

func (m *MemoryHelmClient) LastKnownGood(namespace, name string) (HelmRelease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastGood == nil {
		return HelmRelease{}, false
	}
	rel, ok := m.lastGood[releaseKey(namespace, name)]
	return rel, ok
}

func (m *MemoryHelmClient) appendHistory(key string, rel HelmRelease) {
	if m.history == nil {
		m.history = map[string][]HelmRelease{}
	}
	m.history[key] = append(m.history[key], rel)
}

func (m *MemoryHelmClient) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rels)
}

// HelmInstaller installs or adopts the pinned DPF release through HelmClient.
type HelmInstaller struct {
	Client         HelmClient
	Locks          *ReleaseLocks
	AllowDowngrade bool
	MaxAttempts    int
}

func (h HelmInstaller) locks() *ReleaseLocks {
	if h.Locks != nil {
		return h.Locks
	}
	return defaultReleaseLocks
}

func (h HelmInstaller) attempts() int {
	if h.MaxAttempts > 0 {
		return h.MaxAttempts
	}
	return defaultReleaseAttempts
}

func (h HelmInstaller) remembered(ns, name string) (HelmRelease, bool) {
	if store, ok := h.Client.(lastGoodStore); ok {
		return store.LastKnownGood(ns, name)
	}
	return HelmRelease{}, false
}

func (h HelmInstaller) remember(ns, name string, rel HelmRelease) {
	if store, ok := h.Client.(lastGoodStore); ok {
		store.RememberLastKnownGood(ns, name, rel)
	}
}

func observedResult(action, status string, rel HelmRelease, lastGood, failure string) InstallResult {
	return InstallResult{
		Action:        action,
		Version:       rel.ChartVersion,
		Present:       rel.Name != "",
		Observed:      rel.ChartVersion,
		LastKnownGood: lastGood,
		OpStatus:      status,
		Failure:       failure,
	}
}

// Ensure is the install/adopt/upgrade flow. AMD never touches Helm. An
// existing release is adopted only when ManagedByAdapter is true.
func (h HelmInstaller) Ensure(ctx context.Context, in Intent) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpTimeout, Failure: err.Error()}, err
	}
	if h.Client == nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed}, fmt.Errorf("helm client is required")
	}
	if in.Vendor == VendorAMD {
		return InstallResult{Action: InstallSkipped, Version: in.PinnedVersion}, ErrNoVendorChart
	}

	cfg, err := DefaultInstallConfig(in.Vendor)
	if err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed}, err
	}
	cfg, pol := ApplyVersionPolicy(in, cfg)
	if pol.Skew == SkewInvalidConfig {
		fail := "install config is not the pinned allow-list"
		return InstallResult{Action: InstallSkipped, OpStatus: OpFailed, Failure: fail},
			fmt.Errorf("%s", fail)
	}
	if err := ValidateArtifactRef(cfg); err != nil {
		return InstallResult{Action: InstallSkipped, OpStatus: OpFailed, Failure: err.Error()}, err
	}

	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	unlock := h.locks().acquire(key)
	defer unlock()

	values := SafeHelmValues(cfg)
	rel, err := h.Client.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
	if errors.Is(err, ErrReleaseNotFound) {
		rel, err = h.Client.Install(ctx, cfg, values)
		if err != nil {
			return InstallResult{Action: InstallFailed, OpStatus: OpFailed, Failure: err.Error()}, err
		}
		return observedResult(InstallInstalled, OpSucceeded, rel, "", ""), nil
	}
	if err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed, Failure: err.Error()}, err
	}
	if !rel.ManagedByAdapter() {
		return h.claimOrLeaveUnmanaged(ctx, in, cfg, rel)
	}

	dec := DecideReleaseAction(in.Vendor, rel.ChartVersion, cfg.ChartVersion, h.AllowDowngrade)
	lastGood := rel.ChartVersion
	if saved, ok := h.remembered(cfg.Namespace, cfg.ReleaseName); ok {
		lastGood = saved.ChartVersion
	}
	switch dec.Action {
	case DecisionNoop:
		return observedResult(InstallNoop, OpNoop, rel, lastGood, ""), nil
	case DecisionReject:
		err := ErrUnsupportedVersion
		if strings.Contains(dec.Message, ErrDowngradeForbidden.Error()) {
			err = ErrDowngradeForbidden
		}
		if strings.Contains(dec.Message, ErrMalformedVersion.Error()) {
			err = ErrMalformedVersion
		}
		out := observedResult(InstallSkipped, OpFailed, rel, lastGood, dec.Message)
		return out, err
	}

	h.remember(cfg.Namespace, cfg.ReleaseName, rel)
	return h.upgradeOrRollback(ctx, cfg, values, rel, lastGood)
}

func (h HelmInstaller) upgradeOrRollback(
	ctx context.Context, cfg InstallConfig, values map[string]any, before HelmRelease, lastGood string,
) (InstallResult, error) {
	var lastErr error
	for attempt := 1; attempt <= h.attempts(); attempt++ {
		if err := ctx.Err(); err != nil {
			actual, _ := h.Client.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
			return h.reconcileAfterFailure(ctx, cfg, actual, lastGood, err, OpTimeout)
		}
		rel, err := h.Client.Upgrade(ctx, cfg, values)
		if err == nil {
			if !versionsEqual(rel.ChartVersion, cfg.ChartVersion) || rel.Status != HelmStatusDeployed {
				lastErr = fmt.Errorf("upgrade did not reach the pin")
				continue
			}
			return observedResult(InstallUpgraded, OpSucceeded, rel, lastGood, ""), nil
		}
		lastErr = err
		actual, getErr := h.Client.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
		if getErr == nil && versionsEqual(actual.ChartVersion, cfg.ChartVersion) && actual.Status == HelmStatusDeployed {
			return observedResult(InstallUpgraded, OpSucceeded, actual, lastGood, ""), nil
		}
		if errors.Is(err, ErrReleaseUnknown) {
			if getErr == nil {
				return observedResult(InstallFailed, OpUnknown, actual, lastGood, err.Error()), err
			}
			return InstallResult{
				Action: InstallFailed, OpStatus: OpUnknown, LastKnownGood: lastGood, Failure: err.Error(),
			}, err
		}
		if errors.Is(err, ErrReleaseTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			if getErr == nil {
				return h.reconcileAfterFailure(ctx, cfg, actual, lastGood, err, OpTimeout)
			}
			return InstallResult{
				Action: InstallFailed, OpStatus: OpTimeout, LastKnownGood: lastGood, Failure: err.Error(),
			}, err
		}
	}
	actual, _ := h.Client.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
	return h.rollbackAfterFailure(ctx, cfg, actual, before, lastGood, lastErr)
}

func (h HelmInstaller) reconcileAfterFailure(
	_ context.Context, cfg InstallConfig, actual HelmRelease, lastGood string, err error, status string,
) (InstallResult, error) {
	if actual.Name != "" && versionsEqual(actual.ChartVersion, cfg.ChartVersion) && actual.Status == HelmStatusDeployed {
		return observedResult(InstallUpgraded, OpSucceeded, actual, lastGood, ""), nil
	}
	out := observedResult(InstallFailed, status, actual, lastGood, err.Error())
	if out.Present && out.Version == "" {
		out.Version = actual.ChartVersion
	}
	return out, err
}

func (h HelmInstaller) rollbackAfterFailure(
	ctx context.Context, cfg InstallConfig, actual, before HelmRelease, lastGood string, lastErr error,
) (InstallResult, error) {
	to := lastGood
	if to == "" {
		to = before.ChartVersion
	}
	if to == "" || versionsEqual(to, cfg.ChartVersion) {
		fail := ""
		if lastErr != nil {
			fail = lastErr.Error()
		}
		return observedResult(InstallFailed, OpFailed, actual, lastGood, fail), lastErr
	}
	rb, err := h.Client.Rollback(ctx, cfg, to)
	if err != nil {
		cur := actual
		if got, getErr := h.Client.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName); getErr == nil {
			cur = got
		}
		msg := err.Error()
		if lastErr != nil {
			msg = lastErr.Error() + "; " + msg
		}
		out := observedResult(InstallFailed, OpRollbackFailed, cur, lastGood, msg)
		return out, ErrRollbackFailed
	}
	if !versionsEqual(rb.ChartVersion, to) || rb.Status != HelmStatusDeployed {
		out := observedResult(InstallFailed, OpRollbackFailed, rb, lastGood, "rollback did not restore last-known-good")
		return out, ErrRollbackFailed
	}
	out := observedResult(InstallRolledBack, OpRolledBack, rb, lastGood, "")
	if lastErr != nil {
		out.Failure = lastErr.Error()
	}
	return out, nil
}

// releaseClaimer stamps adapter ownership onto an existing pin. It must
// not install, upgrade, or uninstall.
type releaseClaimer interface {
	ClaimRelease(ctx context.Context, cfg InstallConfig) (HelmRelease, error)
}

func matchesPinnedRelease(rel HelmRelease, cfg InstallConfig) bool {
	return rel.Name == cfg.ReleaseName &&
		rel.Namespace == cfg.Namespace &&
		rel.ChartName == cfg.ChartName &&
		rel.Status == HelmStatusDeployed &&
		versionsEqual(rel.ChartVersion, cfg.ChartVersion)
}

func (h HelmInstaller) claimOrLeaveUnmanaged(
	ctx context.Context, in Intent, cfg InstallConfig, rel HelmRelease,
) (InstallResult, error) {
	if !in.AdoptExisting || !matchesPinnedRelease(rel, cfg) {
		return observedResult(InstallUnmanaged, OpSucceeded, rel, "", ""), nil
	}
	claimer, ok := h.Client.(releaseClaimer)
	if !ok {
		return observedResult(InstallUnmanaged, OpSucceeded, rel, "", ""), nil
	}
	claimed, err := claimer.ClaimRelease(ctx, cfg)
	if err != nil {
		return observedResult(InstallFailed, OpFailed, rel, "", err.Error()), err
	}
	if !claimed.ManagedByAdapter() || !matchesPinnedRelease(claimed, cfg) {
		return observedResult(InstallFailed, OpFailed, claimed, "", "claim did not produce the pinned owned release"),
			ErrReleaseUnmanaged
	}
	return observedResult(InstallAdopted, OpSucceeded, claimed, "", ""), nil
}

func stampAdoptedLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, labels)
	out[ReleaseOwnerLabel] = ReleaseOwner
	out[ReleaseModeLabel] = ReleaseModeAdopted
	return out
}

func (m *MemoryHelmClient) ClaimRelease(_ context.Context, cfg InstallConfig) (HelmRelease, error) {
	if err := requirePinnedConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.rels[key]
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	if !matchesPinnedRelease(existing, cfg) {
		return existing, ErrReleaseUnmanaged
	}
	existing.Labels = stampAdoptedLabels(existing.Labels)
	m.rels[key] = existing
	return existing, nil
}
