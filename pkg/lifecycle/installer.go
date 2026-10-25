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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Installer actions. DetectingInstaller only detects. HelmInstaller
// (helm_client.go) may install or adopt a release this adapter owns.
const (
	InstallDetected   = "detected"
	InstallAbsent     = "absent"
	InstallSkipped    = "skipped"
	InstallInstalled  = "installed"
	InstallAdopted    = "adopted"
	InstallUnmanaged  = "unmanaged"
	InstallNoop       = "noop"
	InstallUpgraded   = "upgraded"
	InstallRolledBack = "rolled_back"
	InstallFailed     = "failed"
)

// nvidiaProbeGVK is the DPF kind whose REST mapping means the NVIDIA operator
// APIs are on the cluster. DPU is the root provisioning object this adapter
// already writes.
const (
	nvidiaProvisioningGroup = "provisioning.dpu.nvidia.com"
	vendorAPIVersion        = "v1alpha1"
	unknownVendorMsg        = "unknown vendor"
	opiConfigGroup          = "config.openshift.io"
	dpuKind                 = "DPU"
	alreadyGoneReason       = "release is already gone"
)

var nvidiaProbeGVK = schema.GroupVersionKind{
	Group:   nvidiaProvisioningGroup,
	Version: vendorAPIVersion,
	Kind:    dpuKind,
}

// amdProbeGVK is the AMD kind whose REST mapping means DSC APIs are present.
// Stubs on a BlueField host are a fake-device risk; detection here is not a
// license to emit AMD CRs.
var amdProbeGVK = schema.GroupVersionKind{
	Group:   "dpu.amd.com",
	Version: vendorAPIVersion,
	Kind:    "DSCDevice",
}

// InstallResult is what the installer observed or wrote.
type InstallResult struct {
	Action        string
	Version       string
	Present       bool
	Observed      string
	LastKnownGood string
	OpStatus      string
	Failure       string
}

// RESTMappingProbe is the one RESTMapper method detection needs.
type RESTMappingProbe interface {
	RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error)
}

// Installer ensures the requested vendor operator is present.
//
// Implementations must be testable without a live package manager. The default
// DetectingInstaller only reads the REST mapper. Helm mutation lives in
// HelmInstaller and adopts only adapter-owned releases.
type Installer interface {
	Ensure(ctx context.Context, in Intent) (InstallResult, error)
}

// DetectingInstaller reports whether the vendor's API is already on the cluster.
type DetectingInstaller struct {
	Mapper RESTMappingProbe
}

// Ensure looks up the vendor's probe GVK. A missing mapping is Absent, not an
// error: the LCM still publishes version skew from the pin the operator set.
func (d DetectingInstaller) Ensure(ctx context.Context, in Intent) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	present := vendorAPIPresent(d.Mapper, in.Vendor)
	out := InstallResult{
		Version: in.PinnedVersion,
		Present: present,
		Action:  InstallAbsent,
	}
	if present {
		out.Action = InstallDetected
	}
	return out, nil
}

func vendorAPIPresent(mapper RESTMappingProbe, vendor string) bool {
	if mapper == nil {
		return false
	}
	gvk, ok := vendorProbeGVK(vendor)
	if !ok {
		return false
	}
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

func vendorProbeGVK(vendor string) (schema.GroupVersionKind, bool) {
	switch vendor {
	case VendorNVIDIA:
		return nvidiaProbeGVK, true
	case VendorAMD:
		return amdProbeGVK, true
	default:
		return schema.GroupVersionKind{}, false
	}
}
