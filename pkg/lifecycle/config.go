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
	"fmt"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Pinned DPF Helm settings. These are the only values DefaultInstallConfig
// will ever return for NVIDIA. A later Helm installer must use this struct
// as-is: no chart URL, extra --set, or namespace from the object.
const (
	DefaultNamespace   = "dpf-operator-system"
	NVIDIAChartName    = "dpf-operator"
	NVIDIAChartRepo    = "https://helm.ngc.nvidia.com/nvidia/doca"
	NVIDIAChartVersion = nvidiaDPFMin
	NVIDIAReleaseName  = "dpf-operator"
	ReleaseOwner       = "opi-nvidia-dpf-adapter"

	helmArgsAnnotation = "lifecycle.opi.nvidia.com/helm-args"
	helmSetAnnotation  = "lifecycle.opi.nvidia.com/helm-set"
	olmCatalogKey      = "lifecycle.opi.nvidia.com/olm-catalog"
	olmPackageKey      = "lifecycle.opi.nvidia.com/olm-package"
	olmSubscriptionKey = "lifecycle.opi.nvidia.com/subscription"
	olmSourceKey       = "lifecycle.opi.nvidia.com/catalog-source"
	chartURLAnnotation = "lifecycle.opi.nvidia.com/chart-url"
)

// forbiddenOverrideKeys are DpuOperatorConfig annotations that would let a
// user pick a chart URL, Helm args, or release namespace. They are rejected
// without echoing the value.
var forbiddenOverrideKeys = []string{
	chartURLAnnotation,
	"lifecycle.opi.nvidia.com/chart-repo",
	"lifecycle.opi.nvidia.com/chart-version",
	helmArgsAnnotation,
	helmSetAnnotation,
	"lifecycle.opi.nvidia.com/release-namespace",
	"lifecycle.opi.nvidia.com/release-name",
	olmCatalogKey,
	olmPackageKey,
	olmSubscriptionKey,
	olmSourceKey,
}

// InstallConfig is the only Helm/install input the LCM will honour.
// HelmArgs must stay empty: user-supplied execution arguments are forbidden.
type InstallConfig struct {
	Vendor       string
	Namespace    string
	ChartName    string
	ChartRepo    string
	ChartVersion string
	ReleaseName  string
	ReleaseOwner string
	HelmArgs     []string
	OLMPublished bool
	OLMPackage   string
	OLMChannel   string
	OLMCatalog   string
}

// DefaultInstallConfig returns the pinned settings for a known vendor.
// The same vendor always yields the same struct (deterministic).
func DefaultInstallConfig(vendor string) (InstallConfig, error) {
	switch strings.ToLower(strings.TrimSpace(vendor)) {
	case VendorNVIDIA:
		return InstallConfig{
			Vendor:       VendorNVIDIA,
			Namespace:    DefaultNamespace,
			ChartName:    NVIDIAChartName,
			ChartRepo:    NVIDIAChartRepo,
			ChartVersion: NVIDIAChartVersion,
			ReleaseName:  NVIDIAReleaseName,
			ReleaseOwner: ReleaseOwner,
		}, nil
	case VendorAMD:
		// Known, unpinned: no Helm chart. Empty chart fields are required.
		return InstallConfig{
			Vendor:       VendorAMD,
			Namespace:    DefaultNamespace,
			ReleaseOwner: ReleaseOwner,
		}, nil
	default:
		return InstallConfig{}, fmt.Errorf("vendor %q is not on the allow-list", vendor)
	}
}

// ValidateInstallConfig rejects anything that is not the pinned allow-list.
func ValidateInstallConfig(c InstallConfig) error {
	if !KnownVendor(c.Vendor) {
		return fmt.Errorf("vendor is not approved")
	}
	if c.Namespace != DefaultNamespace {
		return fmt.Errorf("namespace is not the pinned release namespace")
	}
	if c.ReleaseOwner != ReleaseOwner {
		return fmt.Errorf("release owner is not this adapter")
	}
	if len(c.HelmArgs) != 0 {
		return fmt.Errorf("helm execution arguments are not allowed")
	}
	if c.OLMPublished || c.OLMPackage != "" || c.OLMChannel != "" || c.OLMCatalog != "" {
		return fmt.Errorf("olm package is not published")
	}
	if c.Vendor == VendorAMD {
		if c.ChartName != "" || c.ChartRepo != "" || c.ChartVersion != "" || c.ReleaseName != "" {
			return fmt.Errorf("amd has no pinned helm chart")
		}
		return nil
	}
	if c.ChartName != NVIDIAChartName || c.ChartRepo != NVIDIAChartRepo ||
		c.ChartVersion != NVIDIAChartVersion || c.ReleaseName != NVIDIAReleaseName {
		return fmt.Errorf("helm chart settings are not the pinned nvidia defaults")
	}
	if err := approvedChartRepo(c.ChartRepo); err != nil {
		return err
	}
	return nil
}

func approvedChartRepo(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("chart repo is not an approved https URL")
	}
	if raw != NVIDIAChartRepo {
		return fmt.Errorf("chart repo is not on the allow-list")
	}
	return nil
}

// RejectInstallOverrides reports InvalidConfig when the object tries to
// supply a chart URL, Helm args, or a release namespace. Values are not
// copied into InstallConfig.
func RejectInstallOverrides(cfg *unstructured.Unstructured) error {
	if cfg == nil {
		return nil
	}
	ann := cfg.GetAnnotations()
	for _, k := range forbiddenOverrideKeys {
		if _, ok := ann[k]; ok {
			return fmt.Errorf("install override %s is not allowed", k)
		}
	}
	for k, v := range ann {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "helm-args") || strings.Contains(lk, "helm-set") ||
			strings.Contains(lk, "chart-url") { //nolint:goconst // substring match, not the annotation key
			return fmt.Errorf("install override %s is not allowed", k)
		}
		if looksLikeChartURL(v) && k != VersionAnnotation && k != VendorAnnotation {
			return fmt.Errorf("install override %s is not allowed", k)
		}
	}
	if looksLikeChartURL(ann[VersionAnnotation]) {
		return fmt.Errorf("version pin is not a version")
	}
	return nil
}

func looksLikeChartURL(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return false
	}
	return strings.Contains(s, "://") || strings.HasPrefix(s, "file:") ||
		strings.HasPrefix(s, "oci:") || strings.HasPrefix(s, "--")
}
