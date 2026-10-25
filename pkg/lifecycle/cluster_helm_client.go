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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"
)

// Dedicated probe identity used by disposable-cluster Helm tests.
// Never the live dpf-operator release.
const (
	HelmProbeReleaseName = "opi-lcm-probe"
	HelmProbeNamespace   = "opi-lcm-probe"
	HelmProbeChartName   = "opi-lcm-probe"
	helmStatusSuperseded = "superseded"
	helmInstanceLabel    = "app.kubernetes.io/instance"
)

// ClusterHelmClient observes Helm v3 Secrets like SecretHelmClient
// and mutates a local chart onto the cluster. It does not import
// helm.sh/helm/v3 (that module pulls k8s v0.36.2 or v0.37) and does
// not shell out. The live DPF release name is refused so this client
// cannot install, upgrade, or uninstall dpf-operator.
type ClusterHelmClient struct {
	*SecretHelmClient
	ChartRoot string
	// Locks serialize Install/Upgrade/Rollback/Uninstall per release.
	// Nil uses the process-wide defaultReleaseLocks.
	Locks *ReleaseLocks
}

// NewClusterHelmClient is the production wiring. ChartRoot empty
// means observe/claim only; Install of dpf-operator stays
// ErrHelmMutationNotConfigured.
func NewClusterHelmClient(c client.Client) *ClusterHelmClient {
	return &ClusterHelmClient{SecretHelmClient: NewSecretHelmClient(c)}
}

func (c *ClusterHelmClient) mutationLocks() *ReleaseLocks {
	if c != nil && c.Locks != nil {
		return c.Locks
	}
	return defaultReleaseLocks
}

func (c *ClusterHelmClient) Install(ctx context.Context, cfg InstallConfig, _ map[string]any) (HelmRelease, error) {
	if err := c.guardMutation(cfg); err != nil {
		return HelmRelease{}, err
	}
	unlock := c.mutationLocks().acquire(releaseKey(cfg.Namespace, cfg.ReleaseName))
	defer unlock()
	if _, err := c.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName); err == nil {
		return HelmRelease{}, ErrReleaseExists
	} else if !errorsIsNotFound(err) {
		return HelmRelease{}, err
	}
	return c.writeRevision(ctx, cfg, 1, "")
}

func (c *ClusterHelmClient) Upgrade(ctx context.Context, cfg InstallConfig, _ map[string]any) (HelmRelease, error) {
	if err := c.guardMutation(cfg); err != nil {
		return HelmRelease{}, err
	}
	unlock := c.mutationLocks().acquire(releaseKey(cfg.Namespace, cfg.ReleaseName))
	defer unlock()
	cur, err := c.ownedRelease(ctx, cfg)
	if err != nil {
		return HelmRelease{}, err
	}
	if err := c.markSuperseded(ctx, cfg, cur.Revision); err != nil {
		return HelmRelease{}, err
	}
	return c.writeRevision(ctx, cfg, cur.Revision+1, cur.ChartVersion)
}

func (c *ClusterHelmClient) Rollback(ctx context.Context, cfg InstallConfig, toVersion string) (HelmRelease, error) {
	if err := c.guardMutation(cfg); err != nil {
		return HelmRelease{}, err
	}
	unlock := c.mutationLocks().acquire(releaseKey(cfg.Namespace, cfg.ReleaseName))
	defer unlock()
	if err := ValidateReleaseVersion(toVersion); err != nil {
		return HelmRelease{}, err
	}
	cur, err := c.ownedRelease(ctx, cfg)
	if err != nil {
		return HelmRelease{}, err
	}
	prev, _, err := c.findRevision(ctx, cfg, toVersion)
	if err != nil {
		return HelmRelease{}, err
	}
	if err := c.markSuperseded(ctx, cfg, cur.Revision); err != nil {
		return HelmRelease{}, err
	}
	cfg.ChartName = prev.ChartName
	cfg.ChartVersion = prev.ChartVersion
	return c.writeRevision(ctx, cfg, cur.Revision+1, prev.ChartVersion)
}

func (c *ClusterHelmClient) Uninstall(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	if err := c.guardMutation(cfg); err != nil {
		return HelmRelease{}, err
	}
	unlock := c.mutationLocks().acquire(releaseKey(cfg.Namespace, cfg.ReleaseName))
	defer unlock()
	rel, err := c.ownedRelease(ctx, cfg)
	if err != nil {
		return HelmRelease{}, err
	}
	if !ClassifyReleaseOwnership(rel).AllowsDelete() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	if err := c.deleteReleaseObjects(ctx, cfg); err != nil {
		return HelmRelease{}, err
	}
	if err := c.deleteReleaseSecrets(ctx, cfg); err != nil {
		return HelmRelease{}, err
	}
	return rel, nil
}

func (c *ClusterHelmClient) guardMutation(cfg InstallConfig) error {
	if len(cfg.HelmArgs) != 0 {
		return fmt.Errorf("helm execution arguments are not allowed")
	}
	if cfg.ReleaseOwner != ReleaseOwner {
		return fmt.Errorf("release owner is not this adapter")
	}
	if cfg.ReleaseName == "" || cfg.Namespace == "" || cfg.ChartName == "" || cfg.ChartVersion == "" {
		return fmt.Errorf("helm release identity is incomplete")
	}
	if cfg.ReleaseName == NVIDIAReleaseName {
		return ErrHelmMutationNotConfigured
	}
	if strings.TrimSpace(c.ChartRoot) == "" {
		return ErrHelmMutationNotConfigured
	}
	return nil
}

func (c *ClusterHelmClient) ownedRelease(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	rel, err := c.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
	if err != nil {
		return HelmRelease{}, err
	}
	if !rel.ManagedByAdapter() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	return rel, nil
}

func (c *ClusterHelmClient) writeRevision(ctx context.Context, cfg InstallConfig, rev int, lastGood string) (HelmRelease, error) {
	manifests, err := c.renderChart(cfg)
	if err != nil {
		return HelmRelease{}, err
	}
	if err := c.applyManifests(ctx, manifests); err != nil {
		return HelmRelease{}, err
	}
	rel := OwnedRelease(cfg)
	rel.Revision = rev
	rel.LastKnownGood = lastGood
	if err := c.storeSecret(ctx, cfg, rel, HelmStatusDeployed); err != nil {
		return HelmRelease{}, err
	}
	return c.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
}

func (c *ClusterHelmClient) renderChart(cfg InstallConfig) ([]unstructured.Unstructured, error) {
	dir := filepath.Join(c.ChartRoot, cfg.ChartVersion)
	meta, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	var chart struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := sigsyaml.Unmarshal(meta, &chart); err != nil {
		return nil, err
	}
	if chart.Name != cfg.ChartName || chart.Version != cfg.ChartVersion {
		return nil, fmt.Errorf("chart %s@%s does not match config", chart.Name, chart.Version)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "templates"))
	if err != nil {
		return nil, err
	}
	repl := strings.NewReplacer(
		"{{ .Release.Name }}", cfg.ReleaseName,
		"{{ .Release.Namespace }}", cfg.Namespace,
		"{{ .Chart.Version }}", cfg.ChartVersion,
	)
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	var out []unstructured.Unstructured
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "templates", entry.Name()))
		if err != nil {
			return nil, err
		}
		body := repl.Replace(string(raw))
		obj := unstructured.Unstructured{}
		if _, _, err := dec.Decode([]byte(body), nil, &obj); err != nil {
			return nil, err
		}
		if obj.GetNamespace() == "" {
			obj.SetNamespace(cfg.Namespace)
		}
		out = append(out, obj)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chart %s has no templates", cfg.ChartName)
	}
	return out, nil
}

func (c *ClusterHelmClient) applyManifests(ctx context.Context, objs []unstructured.Unstructured) error {
	for i := range objs {
		if err := c.applyOne(ctx, objs[i].DeepCopy()); err != nil {
			return err
		}
	}
	return nil
}

func (c *ClusterHelmClient) applyOne(ctx context.Context, obj *unstructured.Unstructured) error {
	key := client.ObjectKeyFromObject(obj)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := obj.DeepCopy()
		err := c.Client.Get(ctx, key, existing)
		if apierrors.IsNotFound(err) {
			next := obj.DeepCopy()
			if cerr := c.Client.Create(ctx, next); cerr != nil {
				return alreadyExistsAsConflict(next, cerr)
			}
			return nil
		}
		if err != nil {
			return err
		}
		next := obj.DeepCopy()
		next.SetResourceVersion(existing.GetResourceVersion())
		return c.Client.Update(ctx, next)
	})
}

func alreadyExistsAsConflict(obj client.Object, err error) error {
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	return apierrors.NewConflict(
		schema.GroupResource{Group: gvk.Group, Resource: gvk.Kind},
		obj.GetName(),
		err,
	)
}

func (c *ClusterHelmClient) storeSecret(ctx context.Context, cfg InstallConfig, rel HelmRelease, status string) error {
	labels := map[string]string{
		"name":            cfg.ReleaseName,
		"owner":           "helm",
		"status":          status,
		"version":         strconv.Itoa(rel.Revision),
		ReleaseOwnerLabel: cfg.ReleaseOwner,
		ReleaseModeLabel:  ReleaseModeInstalled,
	}
	payload, err := encodeHelmStorageRelease(helmStorageRelease{
		Name:      cfg.ReleaseName,
		Namespace: cfg.Namespace,
		Version:   rel.Revision,
		Info:      helmStorageInfo{Status: status},
		Chart:     helmStorageChart{Metadata: helmStorageMeta{Name: cfg.ChartName, Version: cfg.ChartVersion}},
		Labels:    labels,
	})
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseSecretName(cfg.ReleaseName, rel.Revision),
			Namespace: cfg.Namespace,
			Labels:    labels,
		},
		Type: helmStorageSecretType,
		Data: map[string][]byte{"release": []byte(payload)},
	}
	key := client.ObjectKeyFromObject(sec)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := &corev1.Secret{}
		err := c.Client.Get(ctx, key, existing)
		if apierrors.IsNotFound(err) {
			next := sec.DeepCopy()
			if cerr := c.Client.Create(ctx, next); cerr != nil {
				return alreadyExistsAsConflict(next, cerr)
			}
			return nil
		}
		if err != nil {
			return err
		}
		next := sec.DeepCopy()
		next.SetResourceVersion(existing.ResourceVersion)
		return c.Client.Update(ctx, next)
	})
}

func (c *ClusterHelmClient) markSuperseded(ctx context.Context, cfg InstallConfig, rev int) error {
	key := client.ObjectKey{Namespace: cfg.Namespace, Name: helmReleaseSecretName(cfg.ReleaseName, rev)}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &corev1.Secret{}
		if err := c.Client.Get(ctx, key, fresh); err != nil {
			return err
		}
		if fresh.Labels == nil {
			fresh.Labels = map[string]string{}
		}
		fresh.Labels["status"] = helmStatusSuperseded
		return c.Client.Update(ctx, fresh)
	})
}

func (c *ClusterHelmClient) findRevision(ctx context.Context, cfg InstallConfig, chartVersion string) (HelmRelease, int, error) {
	var list corev1.SecretList
	if err := c.Client.List(ctx, &list, client.InNamespace(cfg.Namespace), client.MatchingLabels{"name": cfg.ReleaseName}); err != nil {
		return HelmRelease{}, 0, err
	}
	for _, sec := range list.Items {
		rel, err := releaseFromHelmSecret(sec)
		if err != nil {
			continue
		}
		if versionsEqual(rel.ChartVersion, chartVersion) {
			return rel, rel.Revision, nil
		}
	}
	return HelmRelease{}, 0, ErrRollbackFailed
}

func (c *ClusterHelmClient) deleteReleaseObjects(ctx context.Context, cfg InstallConfig) error {
	var cms corev1.ConfigMapList
	if err := c.Client.List(ctx, &cms, client.InNamespace(cfg.Namespace), client.MatchingLabels{helmInstanceLabel: cfg.ReleaseName}); err != nil {
		return err
	}
	for i := range cms.Items {
		if err := c.Client.Delete(ctx, &cms.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *ClusterHelmClient) deleteReleaseSecrets(ctx context.Context, cfg InstallConfig) error {
	var list corev1.SecretList
	if err := c.Client.List(ctx, &list, client.InNamespace(cfg.Namespace), client.MatchingLabels{"name": cfg.ReleaseName}); err != nil {
		return err
	}
	for i := range list.Items {
		if list.Items[i].Type != helmStorageSecretType {
			continue
		}
		if err := c.Client.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func helmReleaseSecretName(release string, rev int) string {
	return fmt.Sprintf("sh.helm.release.v1.%s.v%d", release, rev)
}

func errorsIsNotFound(err error) bool {
	return err != nil && (err == ErrReleaseNotFound || strings.Contains(err.Error(), ErrReleaseNotFound.Error()))
}

func ProbeHelmConfig(version string) InstallConfig {
	return InstallConfig{
		Vendor:       VendorNVIDIA,
		Namespace:    HelmProbeNamespace,
		ChartName:    HelmProbeChartName,
		ChartRepo:    "https://example.invalid/opi-lcm-probe",
		ChartVersion: version,
		ReleaseName:  HelmProbeReleaseName,
		ReleaseOwner: ReleaseOwner,
	}
}
