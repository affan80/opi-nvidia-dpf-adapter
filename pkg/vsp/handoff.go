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

package vsp

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DPUServiceGVK is the DPF intent the translator server-side-applies from
// a chart-bearing ServiceFunctionChain networkFunction. Official DPF
// v26.4 owns the workload through this object (Helm chart), not through
// Vendor.NetworkFunctionService.
//
// https://networking-docs.nvidia.com/dpf/26.4.0/dpuservice-development-guide
var DPUServiceGVK = schema.GroupVersionKind{
	Group:   "svc.dpu.nvidia.com",
	Version: "v1alpha1",
	Kind:    "DPUService",
}

// NetworkFunctionHandoff is the DPF-intent gate. Create/Delete stay thin:
// they never Helm-install or delete a DPUService. They return success
// only after this gate says the corresponding DPF intent was accepted.
type NetworkFunctionHandoff interface {
	CreateIntentAccepted(ctx context.Context, req *pb.NFRequest) error
	DeleteIntentAccepted(ctx context.Context, req *pb.NFRequest) error
}

// RejectHandoff is the default. A missing gate must not look like DPF
// accepted a workload.
type RejectHandoff struct{}

func (RejectHandoff) CreateIntentAccepted(context.Context, *pb.NFRequest) error {
	return fmt.Errorf("network-function create refused: DPF has not accepted a DPUService intent")
}

func (RejectHandoff) DeleteIntentAccepted(context.Context, *pb.NFRequest) error {
	return fmt.Errorf("network-function delete refused: DPF intent cannot be observed")
}

// StaticHandoff is the test/kind seam. A nil error means that side of
// the DPF intent is already accepted.
type StaticHandoff struct {
	CreateErr error
	DeleteErr error
}

func (h StaticHandoff) CreateIntentAccepted(context.Context, *pb.NFRequest) error {
	return h.CreateErr
}

func (h StaticHandoff) DeleteIntentAccepted(context.Context, *pb.NFRequest) error {
	return h.DeleteErr
}

// ClusterHandoff lists live DPUService objects. Existence with a Helm
// chart is "DPF accepted the create intent". Delete is CNI detach: this
// adapter does not uninstall Helm; it only requires that DPF is observable.
type ClusterHandoff struct {
	Client client.Client
}

// NewClusterHandoff watches DPUService objects the translator applies.
func NewClusterHandoff(c client.Client) ClusterHandoff {
	return ClusterHandoff{Client: c}
}

func (h ClusterHandoff) CreateIntentAccepted(ctx context.Context, req *pb.NFRequest) error {
	if err := validateNFRequest(req); err != nil {
		return err
	}
	found, err := h.acceptedDPUServices(ctx)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("network-function create refused: DPF has not accepted a DPUService intent")
	}
	return nil
}

func (h ClusterHandoff) DeleteIntentAccepted(ctx context.Context, req *pb.NFRequest) error {
	if err := validateNFRequest(req); err != nil {
		return err
	}
	if h.Client == nil {
		return fmt.Errorf("network-function delete refused: DPF intent cannot be observed")
	}
	// Detach only. Listing is the proof we talked to DPF; a live
	// DPUService is not a failure — DPF still owns that Helm release.
	if _, err := h.acceptedDPUServices(ctx); err != nil {
		return fmt.Errorf("network-function delete refused: DPF intent cannot be observed: %w", err)
	}
	return nil
}

func (h ClusterHandoff) acceptedDPUServices(ctx context.Context) ([]unstructured.Unstructured, error) {
	if h.Client == nil {
		return nil, fmt.Errorf("observe DPF DPUService intent: no cluster client")
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(DPUServiceGVK.GroupVersion().WithKind("DPUServiceList"))
	if err := h.Client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("observe DPF DPUService intent: %w", err)
	}
	out := make([]unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		item := list.Items[i]
		if item.GetDeletionTimestamp() != nil {
			continue
		}
		chart, _, _ := unstructured.NestedString(item.Object, "spec", "helmChart", "source", "chart")
		if strings.TrimSpace(chart) == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func validateNFRequest(req *pb.NFRequest) error {
	if req == nil || strings.TrimSpace(req.GetInput()) == "" || strings.TrimSpace(req.GetOutput()) == "" {
		return fmt.Errorf("network-function request missing input/output identity")
	}
	return nil
}

func handoffOrReject(h NetworkFunctionHandoff) NetworkFunctionHandoff {
	if h == nil {
		return RejectHandoff{}
	}
	return h
}
