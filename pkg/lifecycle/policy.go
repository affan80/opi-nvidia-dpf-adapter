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

// PolicyResult is the version-window decision for one vendor and one install.
type PolicyResult struct {
	Skew    Skew
	Version string
	Range   SupportedRange
	// Message overrides the default skewStatus text. Used for Blocked so
	// the condition names the missing prerequisite.
	Message string
}

// EvaluatePolicy classifies the installed version against the pinned window.
func EvaluatePolicy(vendor, installed string) PolicyResult {
	r, _ := SupportedRangeFor(vendor)
	return PolicyResult{
		Skew:    ClassifySkew(vendor, installed),
		Version: installed,
		Range:   r,
	}
}
