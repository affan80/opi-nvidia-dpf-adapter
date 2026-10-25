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
	"testing"
)

func TestDecideReleaseActionUpgradeNoopDowngrade(t *testing.T) {
	up := DecideReleaseAction(VendorNVIDIA, "v26.3.9", NVIDIAChartVersion, false)
	if up.Action != DecisionUpgrade {
		t.Fatalf("older installed = %+v, want upgrade", up)
	}
	noop := DecideReleaseAction(VendorNVIDIA, NVIDIAChartVersion, NVIDIAChartVersion, false)
	if noop.Action != DecisionNoop {
		t.Fatalf("same version = %+v, want noop", noop)
	}
	down := DecideReleaseAction(VendorNVIDIA, "v26.4.2", NVIDIAChartVersion, false)
	if down.Action != DecisionReject || down.Message != ErrDowngradeForbidden.Error() {
		t.Fatalf("newer installed = %+v, want reject downgrade", down)
	}
	allowed := DecideReleaseAction(VendorNVIDIA, "v26.4.2", NVIDIAChartVersion, true)
	if allowed.Action != DecisionDowngrade {
		t.Fatalf("explicit downgrade = %+v, want downgrade", allowed)
	}
}

func TestDecideReleaseActionRejectsMalformedAndUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name, installed, desired string
		want                     error
	}{
		{"malformed installed", "latest", NVIDIAChartVersion, ErrMalformedVersion},
		{"url installed", "https://evil.example/chart.tgz", NVIDIAChartVersion, ErrMalformedVersion},
		{"helm flag installed", "--set=image=evil", NVIDIAChartVersion, ErrMalformedVersion},
		{"unsupported desired", "v26.4.0", "v27.1.0", ErrUnsupportedVersion},
		{"malformed desired", "v26.4.0", "not-a-version", ErrMalformedVersion},
		{"off-pin desired in window", "v26.3.9", "v26.4.2", ErrUnsupportedVersion},
	} {
		got := DecideReleaseAction(VendorNVIDIA, tc.installed, tc.desired, false)
		if got.Action != DecisionReject {
			t.Fatalf("%s: %+v, want reject", tc.name, got)
		}
		if err := ValidateDesiredVersion(VendorNVIDIA, tc.desired); tc.want != nil && tc.desired != NVIDIAChartVersion {
			if !errors.Is(err, tc.want) && err == nil {
				t.Fatalf("%s: desired err = %v, want %v", tc.name, err, tc.want)
			}
		}
	}
	if err := ValidateReleaseVersion("latest"); !errors.Is(err, ErrMalformedVersion) {
		t.Fatalf("latest: %v", err)
	}
	if err := ValidateDesiredVersion(VendorNVIDIA, "v27.0.0"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("v27: %v", err)
	}
}

func TestValidateArtifactRefPinnedAndImmutable(t *testing.T) {
	cfg := mustNVIDIAConfig(t)
	if err := ValidateArtifactRef(cfg); err != nil {
		t.Fatal(err)
	}
	evil := cfg
	evil.ChartRepo = "file:///tmp/chart"
	if err := ValidateArtifactRef(evil); !errors.Is(err, ErrDisallowedArtifact) {
		t.Fatalf("file repo: %v", err)
	}
	args := cfg
	args.HelmArgs = []string{"--set", "token=secret"}
	if err := ValidateArtifactRef(args); !errors.Is(err, ErrDisallowedArtifact) {
		t.Fatalf("helm args: %v", err)
	}
	float := cfg
	float.ChartVersion = "latest"
	if err := ValidateArtifactRef(float); err == nil {
		t.Fatal("latest tag must not pass as an immutable pin")
	}
}

func TestCompareReleaseVersionsEdges(t *testing.T) {
	if n, err := compareReleaseVersions(NVIDIAChartVersion, NVIDIAChartVersion); err != nil || n != 0 {
		t.Fatalf("equal: n=%d err=%v", n, err)
	}
	if n, err := compareReleaseVersions("v26.3.9", NVIDIAChartVersion); err != nil || n >= 0 {
		t.Fatalf("older: n=%d err=%v", n, err)
	}
	if _, err := compareReleaseVersions("latest", NVIDIAChartVersion); !errors.Is(err, ErrMalformedVersion) {
		t.Fatalf("latest: %v", err)
	}
	if err := ValidateDesiredVersion(VendorNVIDIA, "v26.4.0-rc.1"); err == nil {
		t.Fatal("prerelease desired must not pass as the pin")
	}
}

func TestSanitizeLogValueRedactsSecrets(t *testing.T) {
	if got := SanitizeLogValue("token", "super-secret"); got != redactedLogValue {
		t.Fatalf("token = %q", got)
	}
	if got := SanitizeLogValue("kubeconfig", "apiVersion: v1"); got != redactedLogValue {
		t.Fatalf("kubeconfig = %q", got)
	}
	if got := SanitizeLogValue("password", "x"); got != redactedLogValue {
		t.Fatalf("password = %q", got)
	}
	if got := SanitizeLogValue("authorization", "Bearer abc"); got != redactedLogValue {
		t.Fatalf("authorization = %q", got)
	}
	if got := SanitizeLogValue("chart", NVIDIAChartName); got != NVIDIAChartName {
		t.Fatalf("chart redacted: %q", got)
	}
}
