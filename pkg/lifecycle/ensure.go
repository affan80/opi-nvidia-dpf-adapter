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
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Result is one pass of EnsureInstalled. Reconcile writes VendorOperatorSupported
// from Policy; it does not write Ready.
type Result struct {
	Intent       Intent
	Preflight    Preflight
	Settings     InstallConfig
	Dependencies DependencyReport
	Install      InstallResult
	Policy       PolicyResult
	Operator     OperatorStatus
	DPU          DPUReport
	Readiness    Readiness
}

func (r *LifecycleManager) installer() Installer {
	if r.Installer != nil {
		return r.Installer
	}
	detect := DetectingInstaller{}
	if r.Client != nil {
		detect.Mapper = r.RESTMapper()
	}
	next := Installer(detect)
	if r.OLM != nil {
		next = olmWithDetect{OLM: OLMInstaller{Client: r.OLM}, Detect: detect}
	}
	if r.Helm != nil {
		return helmWithDetect{
			Helm: HelmInstaller{Client: r.Helm, Locks: r.Locks},
			Next: next,
		}
	}
	return next
}

// helmWithDetect uses Helm when storage exists. A missing release plus
// an observe-only client falls through to OLM (if wired) then
// DetectingInstaller so Kind and the live unmanaged lab do not get a
// second dpf-operator or a fabricated Subscription.
type helmWithDetect struct {
	Helm HelmInstaller
	Next Installer
}

func fallbackInstallErr(err error) bool {
	return errors.Is(err, ErrHelmMutationNotConfigured) ||
		errors.Is(err, ErrOLMNotPresent) ||
		errors.Is(err, ErrOLMMutationNotConfigured) ||
		errors.Is(err, ErrOLMPackageNotPublished) ||
		errors.Is(err, ErrNoVendorChart)
}

func (h helmWithDetect) Ensure(ctx context.Context, in Intent) (InstallResult, error) {
	got, err := h.Helm.Ensure(ctx, in)
	if fallbackInstallErr(err) {
		if h.Next != nil {
			return h.Next.Ensure(ctx, in)
		}
		return DetectingInstaller{}.Ensure(ctx, in)
	}
	return got, err
}

// olmWithDetect observes a Subscription when OLM APIs exist. Missing
// APIs, an unpublished package, or mutation-not-configured fall back
// to DetectingInstaller. Helm storage, when present, already won.
type olmWithDetect struct {
	OLM    OLMInstaller
	Detect DetectingInstaller
}

func (o olmWithDetect) Ensure(ctx context.Context, in Intent) (InstallResult, error) {
	got, err := o.OLM.Ensure(ctx, in)
	if fallbackInstallErr(err) {
		return o.Detect.Ensure(ctx, in)
	}
	return got, err
}

func (r *LifecycleManager) clusterProbe() ClusterProbe {
	if r.Probe != nil {
		return r.Probe
	}
	if r.Client == nil && r.Discovery == nil {
		return nil
	}
	var mapper RESTMappingProbe
	if r.Client != nil {
		mapper = r.RESTMapper()
	}
	return RESTClusterProbe{Mapper: mapper, Discovery: r.Discovery, Client: r.Client}
}

// EnsureInstalled is the LCM install flow: intent → preflight → safe
// config → dependencies → installer → version policy → readiness.
// Reconcile calls this instead of logging "would install". The default
// installer only detects an existing operator. Missing prerequisites
// skip the installer so a detect cannot look like success.
func (r *LifecycleManager) EnsureInstalled(ctx context.Context, cfg *unstructured.Unstructured) (Result, error) {
	in := ParseIntent(cfg)
	pf := RunPreflight(in)
	out := Result{Intent: in, Preflight: pf}
	if pf.Skip {
		out.Install = InstallResult{Action: InstallSkipped}
		return out, nil
	}
	if !pf.OK {
		out.Install = InstallResult{Action: InstallSkipped, Version: in.PinnedVersion}
		out.Policy = PolicyResult{Skew: pf.Reason, Version: in.PinnedVersion}
		out.Readiness = AssessReadiness(in, out.Install, out.Policy, out.Operator)
		return out, nil
	}

	if err := RejectInstallOverrides(cfg); err != nil {
		out.Install = InstallResult{Action: InstallSkipped}
		out.Policy = PolicyResult{Skew: SkewInvalidConfig}
		out.Readiness = AssessReadiness(in, out.Install, out.Policy, out.Operator)
		return out, nil
	}

	settings, err := DefaultInstallConfig(in.Vendor)
	if err != nil {
		out.Install = InstallResult{Action: InstallSkipped}
		out.Policy = PolicyResult{Skew: SkewUnknownVendor}
		out.Readiness = AssessReadiness(in, out.Install, out.Policy, out.Operator)
		return out, nil
	}
	settings, pol := ApplyVersionPolicy(in, settings)
	out.Settings = settings
	out.Policy = pol
	if pol.Skew == SkewInvalidConfig {
		out.Install = InstallResult{Action: InstallSkipped}
		out.Readiness = AssessReadiness(in, out.Install, out.Policy, out.Operator)
		return out, nil
	}

	probe := r.clusterProbe()
	deps := CheckDependencies(ctx, probe, in.Vendor)
	out.Dependencies = deps
	if !deps.OK {
		out.Install = InstallResult{Action: InstallSkipped, Version: in.PinnedVersion}
		out.Policy = PolicyResult{
			Skew:    SkewBlocked,
			Version: in.PinnedVersion,
			Message: deps.Message,
		}
		out.Operator = OperatorStatus{Phase: OperatorInstalling, Message: deps.Message}
		out.Readiness = AssessReadiness(in, out.Install, out.Policy, out.Operator)
		return out, nil
	}

	inst, err := r.installer().Ensure(ctx, in)
	if err != nil {
		return out, err
	}
	out.Install = inst
	if in.PinnedVersion == "" && inst.Version != "" {
		out.Policy = EvaluatePolicy(in.Vendor, inst.Version)
	}
	out.Operator = ObserveOperator(ctx, probe, in.Vendor)
	if in.Vendor == VendorNVIDIA {
		out.DPU = ObserveNVIDIADPUs(ctx, r.Client)
	}
	out.Readiness = AssessReadiness(in, inst, out.Policy, out.Operator)
	return out, nil
}
