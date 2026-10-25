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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Operator install outcomes. These are reasons on VendorOperatorReady,
// not the daemon-owned Ready condition.
const (
	OperatorInstalling = "Installing"
	OperatorReady      = "Ready"
	OperatorDegraded   = "Degraded"
)

// OperatorReadyConditionType is the LCM health condition. The daemon still
// owns type Ready on DpuOperatorConfig.
const OperatorReadyConditionType = "VendorOperatorReady"

// ResourceCheck is one CRD or Deployment the observer looked at.
type ResourceCheck struct {
	Name    string
	Present bool
	Healthy bool
	Message string
}

// namedDeploy is a controller the observer must find after install.
type namedDeploy struct {
	Namespace string
	Name      string
}

// requiredDPFCRDs are the NVIDIA kinds Helm/DPF must register. Missing
// any of these is Installing, not Ready.
var requiredDPFCRDs = []schema.GroupVersionKind{
	{Group: nvidiaProvisioningGroup, Version: vendorAPIVersion, Kind: dpuKind},
	{Group: nvidiaProvisioningGroup, Version: vendorAPIVersion, Kind: "DPUDevice"},
	{Group: nvidiaProvisioningGroup, Version: vendorAPIVersion, Kind: "BFB"},
	{Group: "svc.dpu.nvidia.com", Version: vendorAPIVersion, Kind: "DPUService"},
	{Group: "operator.dpu.nvidia.com", Version: vendorAPIVersion, Kind: "DPFOperatorConfig"},
}

var requiredDPFControllers = []namedDeploy{
	{Namespace: DefaultNamespace, Name: "dpf-operator-controller-manager"},
	{Namespace: DefaultNamespace, Name: "dpf-provisioning-controller-manager"},
	{Namespace: DefaultNamespace, Name: "dpuservice-controller-manager"},
}

// requiredPrereqControllers are the DPF Helm prerequisite Deployments.
// Argo on this lab lives in dpf-operator-system, not argocd.
var requiredPrereqControllers = []namedDeploy{
	{Namespace: "cert-manager", Name: "cert-manager"},
	{Namespace: DefaultNamespace, Name: "argo-cd-argocd-server"},
}

var argoDeployFallbacks = []namedDeploy{
	{Namespace: "argocd", Name: "argocd-server"},
	{Namespace: "argo-cd", Name: "argocd-server"},
}

// OperatorStatus is Installing, Ready, or Degraded after a real look at
// CRDs, controller Deployments, and prerequisite Deployments.
type OperatorStatus struct {
	Phase         string
	Ready         bool
	CRDs          []ResourceCheck
	Controllers   []ResourceCheck
	Prerequisites []ResourceCheck
	Message       string
}

// ObserveOperator is the post-install check. It never installs. Ready is
// true only when every required resource exists and reports healthy.
func ObserveOperator(ctx context.Context, probe ClusterProbe, vendor string) OperatorStatus {
	if err := ctx.Err(); err != nil {
		return OperatorStatus{Phase: OperatorDegraded, Message: err.Error()}
	}
	if probe == nil {
		return OperatorStatus{Phase: OperatorInstalling, Message: "no cluster probe"}
	}
	if vendor == VendorAMD {
		return observeAMD(probe)
	}
	if vendor != VendorNVIDIA {
		return OperatorStatus{Phase: OperatorDegraded, Message: unknownVendorMsg}
	}
	return observeNVIDIA(ctx, probe)
}

func observeAMD(probe ClusterProbe) OperatorStatus {
	crd := ResourceCheck{
		Name:    "dpu.amd.com/DSCDevice",
		Present: probe.HasAPI(amdProbeGVK.Group, amdProbeGVK.Kind, amdProbeGVK.Version),
	}
	out := OperatorStatus{CRDs: []ResourceCheck{crd}}
	if !crd.Present {
		out.Phase = OperatorInstalling
		out.Message = "AMD DSCDevice CRD is not installed"
		return out
	}
	crd.Healthy = true
	out.CRDs[0] = crd
	out.Phase = OperatorReady
	out.Ready = true
	out.Message = "AMD DSC API is present"
	return out
}

func observeNVIDIA(ctx context.Context, probe ClusterProbe) OperatorStatus {
	out := OperatorStatus{}
	for _, gvk := range requiredDPFCRDs {
		present := probe.HasAPI(gvk.Group, gvk.Kind, gvk.Version)
		out.CRDs = append(out.CRDs, ResourceCheck{
			Name:    gvk.Group + "/" + gvk.Kind,
			Present: present,
			Healthy: present,
		})
	}
	for _, d := range requiredDPFControllers {
		out.Controllers = append(out.Controllers, probe.Deployment(ctx, d.Namespace, d.Name))
	}
	out.Prerequisites = observePrereqs(ctx, probe)
	return classifyOperator(out)
}

func observePrereqs(ctx context.Context, probe ClusterProbe) []ResourceCheck {
	cert := probe.Deployment(ctx, "cert-manager", "cert-manager")
	argo := probe.Deployment(ctx, DefaultNamespace, "argo-cd-argocd-server")
	if !argo.Present {
		for _, fb := range argoDeployFallbacks {
			got := probe.Deployment(ctx, fb.Namespace, fb.Name)
			if got.Present {
				argo = got
				break
			}
		}
	}
	if argo.Name == "" {
		argo.Name = "argo-cd"
	}
	return []ResourceCheck{cert, argo}
}

func classifyOperator(out OperatorStatus) OperatorStatus {
	var absent, unhealthy []string
	for _, group := range [][]ResourceCheck{out.CRDs, out.Controllers, out.Prerequisites} {
		for _, c := range group {
			label := c.Name
			if label == "" {
				label = "resource"
			}
			if !c.Present {
				absent = append(absent, label)
				continue
			}
			if !c.Healthy {
				unhealthy = append(unhealthy, label)
			}
		}
	}
	switch {
	case len(absent) > 0:
		out.Phase = OperatorInstalling
		out.Message = "installing: missing " + strings.Join(absent, ", ")
	case len(unhealthy) > 0:
		out.Phase = OperatorDegraded
		out.Message = "degraded: " + strings.Join(unhealthy, ", ")
	default:
		out.Phase = OperatorReady
		out.Ready = true
		out.Message = "DPF CRDs, controllers, and prerequisites are healthy"
	}
	return out
}

func operatorCondition(st OperatorStatus) (status, reason, message string) {
	reason = st.Phase
	if reason == "" {
		reason = OperatorInstalling
	}
	message = st.Message
	if message == "" {
		message = fmt.Sprintf("vendor operator is %s", strings.ToLower(reason))
	}
	if st.Ready && st.Phase == OperatorReady {
		return string(metav1.ConditionTrue), OperatorReady, message
	}
	return string(metav1.ConditionFalse), reason, message
}
