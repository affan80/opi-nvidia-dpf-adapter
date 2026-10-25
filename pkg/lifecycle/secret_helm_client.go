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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// helmStorageSecretType is the Helm v3 Secret type. Read-only: this
// client never creates or deletes storage.
const helmStorageSecretType corev1.SecretType = "helm.sh/release.v1"

// ErrHelmMutationNotConfigured means this cluster client can observe
// Helm storage but cannot install, upgrade, or rollback. Fresh clusters
// fall back to DetectingInstaller instead of creating a second release.
var ErrHelmMutationNotConfigured = errors.New("helm mutation is not configured")

// claimReleaseLocks serialize ClaimRelease without nesting defaultReleaseLocks.
var claimReleaseLocks = newReleaseLocks()

type helmStorageRelease struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Version   int               `json:"version"`
	Info      helmStorageInfo   `json:"info"`
	Chart     helmStorageChart  `json:"chart"`
	Labels    map[string]string `json:"labels"`
}

type helmStorageInfo struct {
	Status string `json:"status"`
}

type helmStorageChart struct {
	Metadata helmStorageMeta `json:"metadata"`
}

type helmStorageMeta struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SecretHelmClient reads Helm v3 release Secrets. It does not import
// the Helm Go module and does not shell out to the helm binary.
//
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;update;patch
type SecretHelmClient struct {
	Client client.Client
}

// NewSecretHelmClient is the observe/claim Helm client. Production
// wiring is NewClusterHelmClient, which embeds this type.
func NewSecretHelmClient(c client.Client) *SecretHelmClient {
	return &SecretHelmClient{Client: c}
}

func helmReleaseListOpts(namespace, name string) []client.ListOption {
	return []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{helmReleaseNameLabel: name},
	}
}

func (s *SecretHelmClient) GetRelease(ctx context.Context, namespace, name string) (HelmRelease, error) {
	if s == nil || s.Client == nil {
		return HelmRelease{}, ErrReleaseNotFound
	}
	var list corev1.SecretList
	err := s.Client.List(ctx, &list, helmReleaseListOpts(namespace, name)...)
	if apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
		return HelmRelease{}, ErrReleaseNotFound
	}
	if err != nil {
		return HelmRelease{}, err
	}
	sec, ok := latestHelmSecret(list.Items)
	if !ok {
		return HelmRelease{}, ErrReleaseNotFound
	}
	rel, err := releaseFromHelmSecret(sec)
	if err != nil {
		return HelmRelease{}, err
	}
	return rel, nil
}

func (s *SecretHelmClient) Install(_ context.Context, _ InstallConfig, _ map[string]any) (HelmRelease, error) {
	return HelmRelease{}, ErrHelmMutationNotConfigured
}

func (s *SecretHelmClient) Upgrade(ctx context.Context, cfg InstallConfig, _ map[string]any) (HelmRelease, error) {
	return s.refuseUnlessOwned(ctx, cfg)
}

func (s *SecretHelmClient) Rollback(ctx context.Context, cfg InstallConfig, _ string) (HelmRelease, error) {
	return s.refuseUnlessOwned(ctx, cfg)
}

func (s *SecretHelmClient) ClaimRelease(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	if err := ValidateInstallConfig(cfg); err != nil {
		return HelmRelease{}, err
	}
	// Dedicated lock: HelmInstaller.Ensure already holds defaultReleaseLocks
	// on this key. Re-acquiring that mutex here deadlocks (sync.Mutex is
	// not reentrant).
	unlock := claimReleaseLocks.acquire(releaseKey(cfg.Namespace, cfg.ReleaseName))
	defer unlock()

	var claimed HelmRelease
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		rel, err := s.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
		if err != nil {
			return err
		}
		if !matchesPinnedRelease(rel, cfg) {
			claimed = rel
			return ErrReleaseUnmanaged
		}
		sec, err := s.latestStorageSecret(ctx, cfg.Namespace, cfg.ReleaseName)
		if err != nil {
			return err
		}
		if alreadyAdoptedLabels(sec.Labels) {
			claimed = rel
			return nil
		}
		fresh := sec.DeepCopy()
		if err := s.Client.Get(ctx, client.ObjectKeyFromObject(fresh), fresh); err != nil {
			return err
		}
		if alreadyAdoptedLabels(fresh.Labels) {
			claimed, err = s.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
			return err
		}
		if fresh.Labels == nil {
			fresh.Labels = map[string]string{}
		}
		fresh.Labels = stampAdoptedLabels(fresh.Labels)
		if err := s.Client.Update(ctx, fresh); err != nil {
			return err
		}
		claimed, err = s.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
		return err
	})
	return claimed, err
}

func alreadyAdoptedLabels(labels map[string]string) bool {
	return labels[ReleaseOwnerLabel] == ReleaseOwner && labels[ReleaseModeLabel] == ReleaseModeAdopted
}

func (s *SecretHelmClient) latestStorageSecret(ctx context.Context, namespace, name string) (corev1.Secret, error) {
	var list corev1.SecretList
	if err := s.Client.List(ctx, &list, helmReleaseListOpts(namespace, name)...); err != nil {
		return corev1.Secret{}, err
	}
	sec, ok := latestHelmSecret(list.Items)
	if !ok {
		return corev1.Secret{}, ErrReleaseNotFound
	}
	return sec, nil
}

func (s *SecretHelmClient) Uninstall(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	rel, err := s.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
	if err != nil {
		return HelmRelease{}, err
	}
	if !ClassifyReleaseOwnership(rel).AllowsDelete() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	return HelmRelease{}, ErrHelmMutationNotConfigured
}

func (s *SecretHelmClient) refuseUnlessOwned(ctx context.Context, cfg InstallConfig) (HelmRelease, error) {
	rel, err := s.GetRelease(ctx, cfg.Namespace, cfg.ReleaseName)
	if err != nil {
		return HelmRelease{}, err
	}
	if !rel.ManagedByAdapter() {
		return HelmRelease{}, ErrReleaseUnmanaged
	}
	return HelmRelease{}, ErrHelmMutationNotConfigured
}

func latestHelmSecret(items []corev1.Secret) (corev1.Secret, bool) {
	var best corev1.Secret
	bestVer := -1
	found := false
	for _, item := range items {
		if item.Type != helmStorageSecretType && item.Type != "" {
			continue
		}
		ver, _ := strconv.Atoi(item.Labels[helmReleaseVersionLabel])
		status := item.Labels[statusKey]
		if status != "" && status != HelmStatusDeployed && status != helmStatusSuperseded {
			continue
		}
		if !found || ver > bestVer || (ver == bestVer && status == HelmStatusDeployed) {
			best = item
			bestVer = ver
			found = true
		}
	}
	return best, found
}

func releaseFromHelmSecret(sec corev1.Secret) (HelmRelease, error) {
	raw := ""
	if sec.Data != nil {
		raw = string(sec.Data["release"])
	}
	doc, err := decodeHelmStorageRelease(raw)
	if err != nil {
		return HelmRelease{}, err
	}
	labels := map[string]string{}
	maps.Copy(labels, sec.Labels)
	maps.Copy(labels, doc.Labels)
	status := doc.Info.Status
	if status == "" {
		status = sec.Labels[statusKey]
	}
	ns := doc.Namespace
	if ns == "" {
		ns = sec.Namespace
	}
	name := doc.Name
	if name == "" {
		name = sec.Labels[helmReleaseNameLabel]
	}
	return HelmRelease{
		Name:         name,
		Namespace:    ns,
		ChartName:    doc.Chart.Metadata.Name,
		ChartVersion: doc.Chart.Metadata.Version,
		Status:       status,
		Revision:     doc.Version,
		Labels:       labels,
	}, nil
}

func encodeHelmStorageRelease(doc helmStorageRelease) (string, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func decodeHelmStorageRelease(data string) (helmStorageRelease, error) {
	var doc helmStorageRelease
	if data == "" {
		return doc, fmt.Errorf("helm release payload is empty")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		raw = []byte(data)
	}
	if unzipped, zerr := gunzipBytes(raw); zerr == nil {
		raw = unzipped
	} else if decoded, berr := base64.StdEncoding.DecodeString(string(raw)); berr == nil {
		if unzipped, zerr = gunzipBytes(decoded); zerr == nil {
			raw = unzipped
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return helmStorageRelease{}, err
	}
	return doc, nil
}

func gunzipBytes(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return nil, fmt.Errorf("not gzip")
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(zr)
}
