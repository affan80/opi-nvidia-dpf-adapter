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
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestClassifySkew(t *testing.T) {
	// The NVIDIA window is [v26.4.0, v27.0.0).
	for _, tc := range []struct {
		name, vendor, installed string
		want                    Skew
	}{
		{"at the inclusive lower bound", "nvidia", "v26.4.0", SkewSupported},
		{"inside the window", "nvidia", "v26.4.2", SkewSupported},
		{"just below the upper bound", "nvidia", "v26.99.99", SkewSupported},
		{"at the exclusive upper bound", "nvidia", "v27.0.0", SkewTooNew},
		{"above the upper bound", "nvidia", "v27.1.0", SkewTooNew},
		{"just below the lower bound", "nvidia", "v26.3.9", SkewTooOld},
		{"well below", "nvidia", "v25.4.0", SkewTooOld},

		// Tolerated spellings: an operator should not have to normalise.
		{"no v prefix", "nvidia", "26.4.0", SkewSupported},
		{"major.minor only", "nvidia", "26.4", SkewSupported},
		{"major only, in range", "nvidia", "26", SkewTooOld},
		{"pre-release is ignored for ordering", "nvidia", "v26.4.0-rc1", SkewSupported},
		{"build metadata is ignored", "nvidia", "v26.4.0+sha.abc123", SkewSupported},
		{"vendor name is case-insensitive", "NVIDIA", "v26.4.0", SkewSupported},
		{"vendor name is trimmed", " nvidia ", "v26.4.0", SkewSupported},

		// Unknown is a distinct outcome from TooOld/TooNew on purpose.
		{"empty version", "nvidia", "", SkewUnknown},
		{"not a version", "nvidia", "latest", SkewUnknown},
		{"too many components", "nvidia", "25.1.0.4", SkewUnknown},
		{"empty component", "nvidia", "25..0", SkewUnknown},
		{"negative component", "nvidia", "25.-1.0", SkewUnknown},

		// A vendor with no pinned window must not be reported as a failure.
		{"unpinned vendor", "amd", "v1.46.0", SkewUnpinned},
		{"unknown vendor", "acme", "v1.0.0", SkewUnpinned},
		{"unpinned beats unparseable", "amd", "nonsense", SkewUnpinned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifySkew(tc.vendor, tc.installed); got != tc.want {
				t.Errorf("ClassifySkew(%q, %q) = %s, want %s",
					tc.vendor, tc.installed, got, tc.want)
			}
		})
	}
}

// TestSupportedRangeIsWellFormed guards the policy table itself: a window whose
// bounds do not parse, or whose min is not below its max, would silently
// classify every cluster as Unknown or TooNew.
func TestKnownVendor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"nvidia", true},
		{"NVIDIA", true},
		{" nvidia ", true},
		{"amd", true},
		{"AMD", true},
		{"acme", false},
		{"dpf", false},
		{"", false},
	} {
		if got := KnownVendor(tc.in); got != tc.want {
			t.Errorf("KnownVendor(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPinnedNVIDIAWindowMatchesVersionsYAML(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "versions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		DPF struct {
			Release       string `json:"release"`
			SupportWindow struct {
				MinInclusive string `json:"minInclusive"`
				MaxExclusive string `json:"maxExclusive"`
			} `json:"supportWindow"`
		} `json:"dpf"`
	}
	if err := yaml.Unmarshal(b, &pin); err != nil {
		t.Fatal(err)
	}
	r, ok := SupportedRangeFor("nvidia")
	if !ok {
		t.Fatal("nvidia has no supportedVersions window")
	}
	if r.MinInclusive != pin.DPF.SupportWindow.MinInclusive || r.MaxExclusive != pin.DPF.SupportWindow.MaxExclusive {
		t.Errorf("supportedVersions[nvidia]=[%s, %s) but config/versions.yaml has [%s, %s)",
			r.MinInclusive, r.MaxExclusive, pin.DPF.SupportWindow.MinInclusive, pin.DPF.SupportWindow.MaxExclusive)
	}
	if got := ClassifySkew("nvidia", pin.DPF.Release); got != SkewSupported {
		t.Errorf("pinned dpf.release %s classifies as %s, want Supported", pin.DPF.Release, got)
	}
}

func TestSupportedRangeIsWellFormed(t *testing.T) {
	if len(supportedVersions) == 0 {
		t.Fatal("no vendor windows pinned; ClassifySkew can only return Unpinned")
	}
	for vendor, r := range supportedVersions {
		min, err := parseVersion(r.MinInclusive)
		if err != nil {
			t.Errorf("%s: MinInclusive %q does not parse: %v", vendor, r.MinInclusive, err)
			continue
		}
		max, err := parseVersion(r.MaxExclusive)
		if err != nil {
			t.Errorf("%s: MaxExclusive %q does not parse: %v", vendor, r.MaxExclusive, err)
			continue
		}
		if min.compare(max) >= 0 {
			t.Errorf("%s: window [%s, %s) is empty or inverted",
				vendor, r.MinInclusive, r.MaxExclusive)
		}
		// The window must actually accept its own lower bound.
		if got := ClassifySkew(vendor, r.MinInclusive); got != SkewSupported {
			t.Errorf("%s: its own MinInclusive %q classifies as %s, want Supported",
				vendor, r.MinInclusive, got)
		}
	}
}

func TestParseVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.2.0", "v1.10.0", -1}, // numeric, not lexicographic
		{"v2.0.0", "v10.0.0", -1},
		{"v25.1", "v25.1.0", 0},
	} {
		a, err := parseVersion(tc.a)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", tc.a, err)
		}
		b, err := parseVersion(tc.b)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", tc.b, err)
		}
		if got := a.compare(b); got != tc.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSkewStatusNeverClaimsFailureForUnknown pins the reporting policy: only a
// definite mismatch is False. Reporting "cannot tell" as a failure would train
// operators to ignore the condition.
func TestSkewStatusNeverClaimsFailureForUnknown(t *testing.T) {
	for _, tc := range []struct {
		skew       Skew
		wantStatus string
	}{
		{SkewSupported, "True"},
		{SkewTooOld, "False"},
		{SkewTooNew, "False"},
		{SkewUnknownVendor, "False"},
		{SkewInvalidConfig, "False"},
		{SkewBlocked, "False"},
		{SkewUnknown, "Unknown"},
		{SkewUnpinned, "Unknown"},
	} {
		t.Run(string(tc.skew), func(t *testing.T) {
			status, message := skewStatus("nvidia", "v1.2.3", tc.skew)
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			if message == "" {
				t.Error("message is empty; the condition would say nothing actionable")
			}
		})
	}
}

// TestSupportedConditionIsNotReady is the single-writer guard for the LCM.
// DpuOperatorConfig's printer column reads the Ready condition and the
// dpu-operator daemon owns it, so the LCM must never write that type.
func TestSupportedConditionIsNotReady(t *testing.T) {
	if SupportedConditionType == "Ready" {
		t.Fatal("the LCM claims the daemon-owned Ready condition on DpuOperatorConfig")
	}
}

func TestSanitizeVendorTokenRejectsInjection(t *testing.T) {
	for _, tc := range []struct {
		in, wantToken string
		wantOK        bool
	}{
		{"", "", true},
		{"nvidia", "nvidia", true},
		{" NVIDIA ", "nvidia", true},
		{"amd", "amd", true},
		{"acme", "acme", true},
		{"../../../etc/passwd", "", false},
		{"nvidia;id", "", false},
		{"https://evil.example", "", false},
		{"file:///etc/passwd", "", false},
		{"$(curl evil)", "", false},
		{"nvidia nvidia", "", false},
		{"this-token-is-way-too-long", "", false},
	} {
		got, ok := SanitizeVendorToken(tc.in)
		if got != tc.wantToken || ok != tc.wantOK {
			t.Errorf("SanitizeVendorToken(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, ok, tc.wantToken, tc.wantOK)
		}
	}
}
