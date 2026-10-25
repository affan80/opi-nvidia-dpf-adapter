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
	"strings"
)

// Preflight is the gate before the installer runs.
type Preflight struct {
	OK      bool
	Skip    bool
	Reason  Skew
	Message string
}

// RunPreflight rejects unknown vendors and skips an empty request.
// It does not talk to the cluster: cluster presence is the installer's job.
func RunPreflight(in Intent) Preflight {
	if in.Vendor == "" {
		return Preflight{
			OK:      true,
			Skip:    true,
			Message: "no vendor requested",
		}
	}
	if !in.Known {
		return Preflight{
			OK:     false,
			Reason: SkewUnknownVendor,
			Message: fmt.Sprintf("vendor %q is not known; allowed values are %s",
				in.Vendor, strings.Join(KnownVendors, ", ")),
		}
	}
	return Preflight{OK: true, Message: "vendor is on the allow-list"}
}
