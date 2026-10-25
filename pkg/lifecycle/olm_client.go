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
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OLM APIs this client may observe. Latest NVIDIA DPF docs install
// through Helm; there is no published OperatorHub package. This path
// reads an existing Subscription/CSV and never creates one.
const (
	olmAPIGroup        = "operators.coreos.com"
	olmAPIVersion      = "v1alpha1"
	subscriptionKind   = "Subscription"
	csvKind            = "ClusterServiceVersion"
	NVIDIAOLMPublished = false
)

// SubscriptionGVK / CSVGVK are the OLM kinds the observer looks up.
var (
	SubscriptionGVK = schema.GroupVersionKind{
		Group: olmAPIGroup, Version: olmAPIVersion, Kind: subscriptionKind,
	}
	CSVGVK = schema.GroupVersionKind{
		Group: olmAPIGroup, Version: olmAPIVersion, Kind: csvKind,
	}
)

// OLM observe / refuse errors. Callers use errors.Is.
var (
	ErrOLMNotPresent            = errors.New("olm apis are not present")
	ErrOLMMutationNotConfigured = errors.New("olm mutation is not configured")
	ErrOLMPackageNotPublished   = errors.New("olm package is not published")
	ErrSubscriptionNotFound     = errors.New("olm subscription not found")
	ErrSubscriptionUnmanaged    = errors.New("olm subscription is not managed by this adapter")
)

// OLMSubscription is one observed vendor-operator install. Identity is
// namespace/name@version, the same shape Helm uses.
type OLMSubscription struct {
	Name      string
	Namespace string
	Package   string
	Channel   string
	Catalog   string
	CSV       string
	Version   string
	Labels    map[string]string
}

// Identity is namespace/name@version.
func (s OLMSubscription) Identity() string {
	return s.Namespace + "/" + s.Name + "@" + s.Version
}

// ManagedByAdapter is the adopt gate. A Subscription that already
// exists but lacks this owner is left alone (unmanaged).
func (s OLMSubscription) ManagedByAdapter() bool {
	if s.Labels == nil {
		return false
	}
	return s.Labels[ReleaseOwnerLabel] == ReleaseOwner
}

// OLMClient is the dedicated OLM surface. No operator-framework SDK,
// no CLI, no user catalog URL.
type OLMClient interface {
	APIsPresent(ctx context.Context) bool
	GetSubscription(ctx context.Context, namespace, name string) (OLMSubscription, error)
	Subscribe(ctx context.Context, cfg InstallConfig) (OLMSubscription, error)
	ClaimSubscription(ctx context.Context, cfg InstallConfig) (OLMSubscription, error)
}

// subscriptionClaimer stamps adapter ownership onto an existing pin.
type subscriptionClaimer interface {
	ClaimSubscription(ctx context.Context, cfg InstallConfig) (OLMSubscription, error)
}

// MemoryOLMClient is the in-process OLM SDK used by tests.
type MemoryOLMClient struct {
	mu             sync.Mutex
	Present        bool
	subs           map[string]OLMSubscription
	SubscribeCalls int
}

// Put stores a Subscription for tests.
func (m *MemoryOLMClient) Put(sub OLMSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subs == nil {
		m.subs = map[string]OLMSubscription{}
	}
	if sub.Labels != nil {
		labels := map[string]string{}
		for k, v := range sub.Labels {
			labels[k] = v
		}
		sub.Labels = labels
	}
	m.subs[releaseKey(sub.Namespace, sub.Name)] = sub
}

func (m *MemoryOLMClient) APIsPresent(_ context.Context) bool {
	if m == nil {
		return false
	}
	return m.Present
}

func (m *MemoryOLMClient) GetSubscription(_ context.Context, namespace, name string) (OLMSubscription, error) {
	if m == nil || !m.Present {
		return OLMSubscription{}, ErrOLMNotPresent
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sub, ok := m.subs[releaseKey(namespace, name)]
	if !ok {
		return OLMSubscription{}, ErrSubscriptionNotFound
	}
	return sub, nil
}

func (m *MemoryOLMClient) Subscribe(_ context.Context, _ InstallConfig) (OLMSubscription, error) {
	m.mu.Lock()
	m.SubscribeCalls++
	m.mu.Unlock()
	return OLMSubscription{}, ErrOLMMutationNotConfigured
}

func (m *MemoryOLMClient) ClaimSubscription(_ context.Context, cfg InstallConfig) (OLMSubscription, error) {
	if m == nil || !m.Present {
		return OLMSubscription{}, ErrOLMNotPresent
	}
	key := releaseKey(cfg.Namespace, cfg.ReleaseName)
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.subs[key]
	if !ok {
		return OLMSubscription{}, ErrSubscriptionNotFound
	}
	if !matchesPinnedOLM(existing, cfg) {
		return existing, ErrSubscriptionUnmanaged
	}
	existing.Labels = stampAdoptedLabels(existing.Labels)
	m.subs[key] = existing
	return existing, nil
}

// ClusterOLMClient reads Subscription and CSV objects. It does not
// import an OLM module and does not create install objects.
//
// +kubebuilder:rbac:groups=operators.coreos.com,resources=subscriptions,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list
type ClusterOLMClient struct {
	Client client.Client
	Mapper RESTMappingProbe
}

// NewClusterOLMClient is the production OLM wiring used by cmd/main.go.
func NewClusterOLMClient(c client.Client, mapper RESTMappingProbe) *ClusterOLMClient {
	return &ClusterOLMClient{Client: c, Mapper: mapper}
}

func (c *ClusterOLMClient) APIsPresent(_ context.Context) bool {
	if c == nil {
		return false
	}
	if c.Mapper != nil {
		_, err := c.Mapper.RESTMapping(SubscriptionGVK.GroupKind(), SubscriptionGVK.Version)
		return err == nil
	}
	return false
}

func (c *ClusterOLMClient) GetSubscription(ctx context.Context, namespace, name string) (OLMSubscription, error) {
	if !c.APIsPresent(ctx) {
		return OLMSubscription{}, ErrOLMNotPresent
	}
	if c.Client == nil {
		return OLMSubscription{}, ErrSubscriptionNotFound
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(SubscriptionGVK)
	err := c.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
	if meta.IsNoMatchError(err) {
		return OLMSubscription{}, ErrOLMNotPresent
	}
	if apierrors.IsNotFound(err) {
		return OLMSubscription{}, ErrSubscriptionNotFound
	}
	if err != nil {
		return OLMSubscription{}, err
	}
	sub := subscriptionFromUnstructured(obj)
	c.fillCSVVersion(ctx, &sub)
	return sub, nil
}

func (c *ClusterOLMClient) Subscribe(_ context.Context, _ InstallConfig) (OLMSubscription, error) {
	return OLMSubscription{}, ErrOLMMutationNotConfigured
}

func (c *ClusterOLMClient) ClaimSubscription(ctx context.Context, cfg InstallConfig) (OLMSubscription, error) {
	sub, err := c.GetSubscription(ctx, cfg.Namespace, cfg.ReleaseName)
	if err != nil {
		return OLMSubscription{}, err
	}
	if !matchesPinnedOLM(sub, cfg) {
		return sub, ErrSubscriptionUnmanaged
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(SubscriptionGVK)
	key := client.ObjectKey{Namespace: cfg.Namespace, Name: cfg.ReleaseName}
	if err := c.Client.Get(ctx, key, obj); err != nil {
		return OLMSubscription{}, err
	}
	obj.SetLabels(stampAdoptedLabels(obj.GetLabels()))
	if err := c.Client.Update(ctx, obj); err != nil {
		return OLMSubscription{}, err
	}
	return c.GetSubscription(ctx, cfg.Namespace, cfg.ReleaseName)
}

func (c *ClusterOLMClient) fillCSVVersion(ctx context.Context, sub *OLMSubscription) {
	if c.Client == nil || sub.CSV == "" {
		return
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CSVGVK)
	err := c.Client.Get(ctx, client.ObjectKey{Namespace: sub.Namespace, Name: sub.CSV}, obj)
	if err != nil {
		return
	}
	ver, _, _ := unstructured.NestedString(obj.Object, "spec", "version")
	if ver != "" {
		sub.Version = ver
	}
}

func subscriptionFromUnstructured(obj *unstructured.Unstructured) OLMSubscription {
	pkg, _, _ := unstructured.NestedString(obj.Object, "spec", "name")
	channel, _, _ := unstructured.NestedString(obj.Object, "spec", "channel")
	source, _, _ := unstructured.NestedString(obj.Object, "spec", "source")
	csv, _, _ := unstructured.NestedString(obj.Object, "status", "installedCSV")
	if csv == "" {
		csv, _, _ = unstructured.NestedString(obj.Object, "status", "currentCSV")
	}
	return OLMSubscription{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Package:   pkg,
		Channel:   channel,
		Catalog:   source,
		CSV:       csv,
		Version:   versionFromCSVRef(csv),
		Labels:    obj.GetLabels(),
	}
}

func versionFromCSVRef(csv string) string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return ""
	}
	if i := strings.LastIndex(csv, ".v"); i >= 0 {
		return csv[i+1:]
	}
	if i := strings.LastIndex(csv, "."); i >= 0 {
		rest := csv[i+1:]
		if _, err := parseVersion(rest); err == nil {
			return rest
		}
	}
	if _, err := parseVersion(csv); err == nil {
		return csv
	}
	return ""
}

func matchesPinnedOLM(sub OLMSubscription, cfg InstallConfig) bool {
	return sub.Name == cfg.ReleaseName &&
		sub.Namespace == cfg.Namespace &&
		versionsEqual(sub.Version, cfg.ChartVersion)
}

func observedOLMResult(action, status string, sub OLMSubscription, failure string) InstallResult {
	return InstallResult{
		Action:   action,
		Version:  sub.Version,
		Present:  sub.Name != "",
		Observed: sub.Version,
		OpStatus: status,
		Failure:  failure,
	}
}

// OLMInstaller observes or claims a matching Subscription. It never
// creates one. AMD has no package and is skipped.
type OLMInstaller struct {
	Client OLMClient
}

func (o OLMInstaller) Ensure(ctx context.Context, in Intent) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpTimeout, Failure: err.Error()}, err
	}
	if o.Client == nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed}, ErrOLMNotPresent
	}
	if in.Vendor == VendorAMD {
		return InstallResult{Action: InstallSkipped, Version: in.PinnedVersion}, ErrNoVendorChart
	}
	if !o.Client.APIsPresent(ctx) {
		return InstallResult{}, ErrOLMNotPresent
	}

	cfg, err := DefaultInstallConfig(in.Vendor)
	if err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed}, err
	}
	cfg, pol := ApplyVersionPolicy(in, cfg)
	if pol.Skew == SkewInvalidConfig {
		return InstallResult{Action: InstallSkipped, OpStatus: OpFailed, Failure: "install config is not the pinned allow-list"},
			ErrOLMPackageNotPublished
	}

	sub, err := o.Client.GetSubscription(ctx, cfg.Namespace, cfg.ReleaseName)
	if errors.Is(err, ErrOLMNotPresent) {
		return InstallResult{}, err
	}
	if errors.Is(err, ErrSubscriptionNotFound) {
		if !NVIDIAOLMPublished {
			return InstallResult{}, ErrOLMPackageNotPublished
		}
		return InstallResult{}, ErrOLMMutationNotConfigured
	}
	if err != nil {
		return InstallResult{Action: InstallFailed, OpStatus: OpFailed, Failure: err.Error()}, err
	}
	if !sub.ManagedByAdapter() {
		return o.claimOrLeaveUnmanaged(ctx, in, cfg, sub)
	}
	return observedOLMResult(InstallNoop, OpNoop, sub, ""), nil
}

func (o OLMInstaller) claimOrLeaveUnmanaged(
	ctx context.Context, in Intent, cfg InstallConfig, sub OLMSubscription,
) (InstallResult, error) {
	if !in.AdoptExisting || !matchesPinnedOLM(sub, cfg) {
		return observedOLMResult(InstallUnmanaged, OpSucceeded, sub, ""), nil
	}
	claimer, ok := o.Client.(subscriptionClaimer)
	if !ok {
		return observedOLMResult(InstallUnmanaged, OpSucceeded, sub, ""), nil
	}
	claimed, err := claimer.ClaimSubscription(ctx, cfg)
	if err != nil {
		return observedOLMResult(InstallFailed, OpFailed, sub, err.Error()), err
	}
	if !claimed.ManagedByAdapter() || !matchesPinnedOLM(claimed, cfg) {
		return observedOLMResult(InstallFailed, OpFailed, claimed, "claim did not produce the pinned owned subscription"),
			ErrSubscriptionUnmanaged
	}
	return observedOLMResult(InstallAdopted, OpSucceeded, claimed, ""), nil
}
