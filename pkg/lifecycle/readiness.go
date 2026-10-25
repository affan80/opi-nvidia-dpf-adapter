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

// Readiness is whether the vendor operator is usable for translation.
// It is not the Ready condition: that type is owned by the dpu-operator daemon.
type Readiness struct {
	Ready   bool
	Message string
}

// AssessReadiness is true only when the vendor is known, the version
// window allows it, the installer saw the operator, and ObserveOperator
// found healthy CRDs, controllers, and prerequisites.
func AssessReadiness(in Intent, inst InstallResult, pol PolicyResult, st OperatorStatus) Readiness {
	if !in.Known {
		return Readiness{Ready: false, Message: unknownVendorMsg}
	}
	switch pol.Skew {
	case SkewTooOld, SkewTooNew, SkewUnknownVendor, SkewInvalidConfig, SkewBlocked:
		return Readiness{Ready: false, Message: "version policy rejected this install"}
	}
	if !inst.Present {
		return Readiness{Ready: false, Message: "vendor operator API not detected"}
	}
	if !st.Ready || st.Phase != OperatorReady {
		msg := st.Message
		if msg == "" {
			msg = "vendor operator CRDs or controllers are not healthy"
		}
		return Readiness{Ready: false, Message: msg}
	}
	return Readiness{Ready: true, Message: st.Message}
}
