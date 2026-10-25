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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// IntentSource is which enablement field ParseIntent used.
type IntentSource string

const (
	// IntentNone means neither spec.vendor nor the annotation was set.
	IntentNone IntentSource = ""
	// IntentSpec means spec.vendor won (it always beats the annotation).
	IntentSpec IntentSource = "spec"
	// IntentAnnotation means the annotation was used because spec.vendor was empty.
	IntentAnnotation IntentSource = "annotation"

	// AdoptExistingAnnotation is the explicit consent to claim a Helm
	// release that already matches the pin. Empty means leave owner=helm
	// unmanaged. Values: true | pin.
	AdoptExistingAnnotation = "lifecycle.opi.nvidia.com/adopt-existing"
	annotationTrue          = "true"
)

const invalidVendor = "invalid"

// Intent is the parsed install request from one DpuOperatorConfig.
type Intent struct {
	Vendor        string
	PinnedVersion string
	Source        IntentSource
	Known         bool
	AdoptExisting bool
}

// ParseIntent reads vendor and the optional version pin from a DpuOperatorConfig.
// spec.vendor wins; the annotation is the fallback. Invalid tokens become
// "invalid" so KnownVendor rejects them without echoing a path or URL.
func ParseIntent(cfg *unstructured.Unstructured) Intent {
	if cfg == nil {
		return Intent{}
	}
	in := Intent{
		PinnedVersion: strings.TrimSpace(cfg.GetAnnotations()[VersionAnnotation]),
		AdoptExisting: adoptExistingRequested(cfg),
	}
	specRaw, _, _ := unstructured.NestedString(cfg.Object, vendorField...)
	if strings.TrimSpace(specRaw) != "" {
		in.Source = IntentSpec
		in.Vendor = sanitizedVendor(specRaw)
		in.Known = KnownVendor(in.Vendor)
		return in
	}
	annRaw := cfg.GetAnnotations()[VendorAnnotation]
	if strings.TrimSpace(annRaw) != "" {
		in.Source = IntentAnnotation
		in.Vendor = sanitizedVendor(annRaw)
		in.Known = KnownVendor(in.Vendor)
		return in
	}
	return in
}

func adoptExistingRequested(cfg *unstructured.Unstructured) bool {
	if cfg == nil {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(cfg.GetAnnotations()[AdoptExistingAnnotation]))
	return v == annotationTrue || v == "pin"
}

func sanitizedVendor(raw string) string {
	token, ok := SanitizeVendorToken(raw)
	if !ok {
		return invalidVendor
	}
	return token
}

// ResolveVendor is the vendor token from ParseIntent. Kept so callers that
// only need the name do not have to unpack Intent.
func ResolveVendor(cfg *unstructured.Unstructured) string {
	return ParseIntent(cfg).Vendor
}
