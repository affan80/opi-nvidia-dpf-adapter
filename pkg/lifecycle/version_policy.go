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
	"errors"
	"fmt"
	"strings"
)

// Release comparison outcomes. Desired is always the pinned chart version.
const (
	DecisionUpgrade   = "upgrade"
	DecisionNoop      = "noop"
	DecisionDowngrade = "downgrade"
	DecisionReject    = "reject"
)

// Version decision / artifact errors. Callers use errors.Is.
var (
	ErrMalformedVersion   = errors.New("release version is malformed")
	ErrUnsupportedVersion = errors.New("release version is not in the pinned window")
	ErrDowngradeForbidden = errors.New("downgrade is not allowed by policy")
	ErrDisallowedArtifact = errors.New("artifact reference is not the pinned immutable chart")
)

// VersionDecision is installed vs desired for one owned release.
type VersionDecision struct {
	Action    string
	Installed string
	Desired   string
	Message   string
}

// ApplyVersionPolicy binds an intent to the pinned InstallConfig.
//
// The Helm chart version and repo never come from the object: they stay
// the defaults. An optional version annotation is only used to classify
// what is already installed. Empty pin → the pinned chart version, so
// two reconciles with the same vendor produce the same settings.
func ApplyVersionPolicy(in Intent, settings InstallConfig) (InstallConfig, PolicyResult) {
	if err := ValidateInstallConfig(settings); err != nil {
		return settings, PolicyResult{Skew: SkewInvalidConfig}
	}
	installed := strings.TrimSpace(in.PinnedVersion)
	if looksLikeChartURL(installed) {
		return settings, PolicyResult{Skew: SkewInvalidConfig}
	}
	// ChartVersion stays the pin. Classification uses the annotation only:
	// an empty pin is Unknown (we have not inspected the cluster), not a
	// claim that the default chart is already running.
	return settings, EvaluatePolicy(in.Vendor, installed)
}

// ValidateReleaseVersion rejects URLs, flags, and unparseable versions.
// It does not require the version to be inside the window: that is
// ClassifySkew / ValidateDesiredVersion.
func ValidateReleaseVersion(ver string) error {
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return ErrMalformedVersion
	}
	if looksLikeChartURL(ver) {
		return ErrMalformedVersion
	}
	if strings.ContainsAny(ver, " \t\n;|$`") {
		return ErrMalformedVersion
	}
	if _, err := parseVersion(ver); err != nil {
		return ErrMalformedVersion
	}
	return nil
}

// ValidateDesiredVersion requires a parseable version inside the vendor
// window. NVIDIA desired must also be the pinned chart version.
func ValidateDesiredVersion(vendor, desired string) error {
	if err := ValidateReleaseVersion(desired); err != nil {
		return err
	}
	if vendor == VendorAMD {
		return fmt.Errorf("amd has no pinned helm chart")
	}
	if ClassifySkew(vendor, desired) != SkewSupported {
		return ErrUnsupportedVersion
	}
	if vendor == VendorNVIDIA && strings.TrimSpace(desired) != NVIDIAChartVersion {
		return ErrUnsupportedVersion
	}
	return nil
}

// ValidateArtifactRef is the immutable chart pin: https allow-list repo,
// exact version, no helm args, no floating tag.
func ValidateArtifactRef(cfg InstallConfig) error {
	if err := ValidateInstallConfig(cfg); err != nil {
		return ErrDisallowedArtifact
	}
	if cfg.Vendor == VendorAMD {
		return fmt.Errorf("amd has no pinned helm chart")
	}
	if err := ValidateDesiredVersion(cfg.Vendor, cfg.ChartVersion); err != nil {
		return err
	}
	if err := approvedChartRepo(cfg.ChartRepo); err != nil {
		return ErrDisallowedArtifact
	}
	return nil
}

// DecideReleaseAction compares an observed installed version to the
// pinned desired version. Downgrade is rejected unless allowDowngrade.
func DecideReleaseAction(vendor, installed, desired string, allowDowngrade bool) VersionDecision {
	out := VersionDecision{Installed: installed, Desired: desired, Action: DecisionReject}
	if err := ValidateDesiredVersion(vendor, desired); err != nil {
		out.Message = err.Error()
		return out
	}
	if err := ValidateReleaseVersion(installed); err != nil {
		out.Message = err.Error()
		return out
	}
	cmp, err := compareReleaseVersions(installed, desired)
	if err != nil {
		out.Message = err.Error()
		return out
	}
	switch {
	case cmp == 0:
		out.Action = DecisionNoop
		out.Message = "installed version is the pin"
	case cmp < 0:
		out.Action = DecisionUpgrade
		out.Message = "upgrade to the pinned release"
	default:
		if allowDowngrade {
			out.Action = DecisionDowngrade
			out.Message = "downgrade explicitly allowed"
			return out
		}
		out.Action = DecisionReject
		out.Message = ErrDowngradeForbidden.Error()
	}
	return out
}

func compareReleaseVersions(a, b string) (int, error) {
	va, err := parseVersion(a)
	if err != nil {
		return 0, ErrMalformedVersion
	}
	vb, err := parseVersion(b)
	if err != nil {
		return 0, ErrMalformedVersion
	}
	return va.compare(vb), nil
}

func versionsEqual(a, b string) bool {
	n, err := compareReleaseVersions(a, b)
	return err == nil && n == 0
}
