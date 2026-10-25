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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestObserveOperatorInstallingWhenCRDMissing(t *testing.T) {
	st := ObserveOperator(context.Background(), StaticProbe{
		Version:     SupportedKubernetesVersion,
		Present:     nvidiaReadyAPIs(),
		Deployments: healthyDeployments(),
	}, VendorNVIDIA)
	if st.Phase != OperatorInstalling || st.Ready {
		t.Fatalf("missing CRDs = %+v, want Installing", st)
	}
}

func TestObserveOperatorInstallingWhenDeploymentMissing(t *testing.T) {
	st := ObserveOperator(context.Background(), StaticProbe{
		Version:     SupportedKubernetesVersion,
		Present:     readyNVIDIAAPIs(),
		Deployments: healthyPrereqsOnly(),
	}, VendorNVIDIA)
	if st.Phase != OperatorInstalling || st.Ready {
		t.Fatalf("missing controller = %+v, want Installing", st)
	}
}

func TestObserveOperatorDegradedWhenControllerUnhealthy(t *testing.T) {
	deps := healthyDeployments()
	deps[releaseKey(DefaultNamespace, "dpf-operator-controller-manager")] = ResourceCheck{
		Name: "dpf-operator-controller-manager", Present: true, Healthy: false, Message: "ready 0/1",
	}
	st := ObserveOperator(context.Background(), StaticProbe{
		Version:     SupportedKubernetesVersion,
		Present:     readyNVIDIAAPIs(),
		Deployments: deps,
	}, VendorNVIDIA)
	if st.Phase != OperatorDegraded || st.Ready {
		t.Fatalf("unhealthy controller = %+v, want Degraded", st)
	}
}

func TestObserveOperatorReadyWhenAllHealthy(t *testing.T) {
	st := ObserveOperator(context.Background(), ReadyClusterProbe(), VendorNVIDIA)
	if st.Phase != OperatorReady || !st.Ready {
		t.Fatalf("healthy cluster = %+v, want Ready", st)
	}
}

func TestObserveOperatorAMDSkippedChartStillNeedsCRD(t *testing.T) {
	st := ObserveOperator(context.Background(), StaticProbe{Version: SupportedKubernetesVersion}, VendorAMD)
	if st.Phase != OperatorInstalling || st.Ready {
		t.Fatalf("AMD without DSC CRD = %+v, want Installing", st)
	}
	ready := ObserveOperator(context.Background(), StaticProbe{
		Version: SupportedKubernetesVersion,
		Present: map[string]bool{apiKey(amdProbeGVK.Group, amdProbeGVK.Kind): true},
	}, VendorAMD)
	if ready.Phase != OperatorReady || !ready.Ready {
		t.Fatalf("AMD with DSC CRD = %+v, want Ready", ready)
	}
}

func TestAssessReadinessRequiresHealthyOperator(t *testing.T) {
	in := Intent{Vendor: VendorNVIDIA, Known: true}
	present := InstallResult{Present: true, Action: InstallDetected}
	pol := PolicyResult{Skew: SkewSupported}
	if got := AssessReadiness(in, present, pol, OperatorStatus{}); got.Ready {
		t.Fatal("API present without healthy observation must not be Ready")
	}
	if got := AssessReadiness(in, present, pol, OperatorStatus{Phase: OperatorInstalling}); got.Ready {
		t.Fatal("Installing must not be Ready")
	}
	if got := AssessReadiness(in, present, pol, OperatorStatus{Phase: OperatorDegraded}); got.Ready {
		t.Fatal("Degraded must not be Ready")
	}
	if got := AssessReadiness(in, present, pol, OperatorStatus{Phase: OperatorReady, Ready: true}); !got.Ready {
		t.Fatal("healthy observation must be Ready")
	}
	if got := AssessReadiness(in, InstallResult{}, pol, OperatorStatus{Phase: OperatorReady, Ready: true}); got.Ready {
		t.Fatal("absent install must not be Ready")
	}
}

func TestEnsureInstalledReadyOnlyWhenHealthy(t *testing.T) {
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	detected := InstallResult{Action: InstallDetected, Present: true, Version: NVIDIAChartVersion}

	healthy, err := (&LifecycleManager{
		Installer: &recordingInstaller{result: detected},
		Probe:     ReadyClusterProbe(),
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if healthy.Operator.Phase != OperatorReady || !healthy.Readiness.Ready {
		t.Fatalf("healthy = phase=%s ready=%v", healthy.Operator.Phase, healthy.Readiness.Ready)
	}

	deps := healthyDeployments()
	deps[releaseKey(DefaultNamespace, "dpf-provisioning-controller-manager")] = ResourceCheck{
		Name: "dpf-provisioning-controller-manager", Present: true, Healthy: false,
	}
	degraded, err := (&LifecycleManager{
		Installer: &recordingInstaller{result: detected},
		Probe: StaticProbe{
			Version:     SupportedKubernetesVersion,
			Present:     readyNVIDIAAPIs(),
			Deployments: deps,
		},
	}).EnsureInstalled(context.Background(), obj)
	if err != nil {
		t.Fatal(err)
	}
	if degraded.Operator.Phase != OperatorDegraded || degraded.Readiness.Ready {
		t.Fatalf("degraded = phase=%s ready=%v, LCM must not report Ready",
			degraded.Operator.Phase, degraded.Readiness.Ready)
	}
}

func TestReconcileWritesVendorOperatorReady(t *testing.T) {
	sch := lcmScheme()
	obj := configWithVersion(VendorNVIDIA, NVIDIAChartVersion)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(obj).WithStatusSubresource(obj).Build()
	r := &LifecycleManager{
		Client: cl, Scheme: sch,
		Installer: &recordingInstaller{result: InstallResult{
			Action: InstallDetected, Present: true, Version: NVIDIAChartVersion,
		}},
		Probe: ReadyClusterProbe(),
	}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cfg"},
	}); err != nil {
		t.Fatal(err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(DpuOperatorConfigGVK)
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "cfg"}, got); err != nil {
		t.Fatal(err)
	}
	ready := conditionOfType(t, got, OperatorReadyConditionType)
	if ready == nil || ready["status"] != "True" || ready["reason"] != OperatorReady {
		t.Fatalf("VendorOperatorReady = %v, want True/Ready", ready)
	}
	if c := conditionOfType(t, got, "Ready"); c != nil {
		t.Fatalf("LCM must not write daemon Ready: %v", c)
	}
}

func readyNVIDIAAPIs() map[string]bool {
	present := nvidiaReadyAPIs()
	for _, crd := range requiredDPFCRDs {
		present[apiKey(crd.Group, crd.Kind)] = true
	}
	return present
}

func healthyDeployments() map[string]ResourceCheck {
	out := map[string]ResourceCheck{}
	for _, d := range append(requiredDPFControllers, requiredPrereqControllers...) {
		out[releaseKey(d.Namespace, d.Name)] = ResourceCheck{
			Name: d.Name, Present: true, Healthy: true,
		}
	}
	return out
}

func healthyPrereqsOnly() map[string]ResourceCheck {
	out := map[string]ResourceCheck{}
	for _, d := range requiredPrereqControllers {
		out[releaseKey(d.Namespace, d.Name)] = ResourceCheck{
			Name: d.Name, Present: true, Healthy: true,
		}
	}
	return out
}
