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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/discovery"
)

func TestCreateNetworkFunctionRefusesBeforeDPFIntent(t *testing.T) {
	ctx, conn, errc, cancel := startTestServer(t)
	_, err := pb.NewNetworkFunctionServiceClient(conn).CreateNetworkFunction(ctx, validNF())
	if err == nil || !strings.Contains(err.Error(), "DPUService") {
		t.Fatalf("create before intent = %v", err)
	}
	_, err = pb.NewNetworkFunctionServiceClient(conn).DeleteNetworkFunction(ctx, validNF())
	if err == nil || !strings.Contains(err.Error(), "DPF") {
		t.Fatalf("delete before observe = %v", err)
	}
	waitStop(t, cancel, errc)
}

func TestNetworkFunctionHandoffAfterAcceptedIntent(t *testing.T) {
	ctx, conn, errc, cancel := startServerWithHandoff(t, StaticHandoff{})
	if _, err := pb.NewNetworkFunctionServiceClient(conn).CreateNetworkFunction(ctx, validNF()); err != nil {
		t.Fatalf("create after accepted intent: %v", err)
	}
	if _, err := pb.NewNetworkFunctionServiceClient(conn).DeleteNetworkFunction(ctx, validNF()); err != nil {
		t.Fatalf("detach after DPF observe: %v", err)
	}
	waitStop(t, cancel, errc)
}

func TestClusterHandoffCreateRequiresAcceptedDPUService(t *testing.T) {
	scheme := newDPUServiceScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	h := NewClusterHandoff(c)
	if err := h.CreateIntentAccepted(context.Background(), validNF()); err == nil {
		t.Fatal("empty cluster must not report create success")
	}

	if err := c.Create(context.Background(), acceptedDPUService("hbn")); err != nil {
		t.Fatal(err)
	}
	if err := h.CreateIntentAccepted(context.Background(), validNF()); err != nil {
		t.Fatalf("accepted DPUService: %v", err)
	}
	if err := h.DeleteIntentAccepted(context.Background(), validNF()); err != nil {
		t.Fatalf("detach while DPUService lives: %v", err)
	}

	stub := acceptedDPUService("image-only")
	_ = unstructured.SetNestedField(stub.Object, "", "spec", "helmChart", "source", "chart")
	c2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stub).Build()
	if err := NewClusterHandoff(c2).CreateIntentAccepted(context.Background(), validNF()); err == nil {
		t.Fatal("image-only / empty chart is not a DPF create intent")
	}

	dying := acceptedDPUService("going")
	dying.SetFinalizers([]string{"handoff-test"})
	now := metav1.NewTime(time.Now())
	dying.SetDeletionTimestamp(&now)
	c3 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dying).Build()
	if err := NewClusterHandoff(c3).CreateIntentAccepted(context.Background(), validNF()); err == nil {
		t.Fatal("deleting DPUService is not an accepted create intent")
	}

	if err := NewClusterHandoff(c).CreateIntentAccepted(context.Background(), &pb.NFRequest{}); err == nil {
		t.Fatal("empty NFRequest")
	}
	if err := (ClusterHandoff{}).DeleteIntentAccepted(context.Background(), validNF()); err == nil {
		t.Fatal("nil client must not report delete success")
	}
}

func TestVSPRBACCanObserveDPUServiceIntent(t *testing.T) {
	b := readRepoFile(t, "config", "vsp", "rbac.yaml")
	doc := string(b)
	if !strings.Contains(doc, "svc.dpu.nvidia.com") || !strings.Contains(doc, "dpuservices") {
		t.Fatal("VSP RBAC must list DPUServices or Create always looks like no intent")
	}
	if strings.Contains(doc, "create") && strings.Contains(doc, "dpuservices") {
		// The DPU block uses patch/update; the DPUService block must stay observe-only.
		svcBlock := doc[strings.Index(doc, "svc.dpu.nvidia.com"):]
		for _, verb := range []string{"create", "update", "patch", "delete"} {
			if strings.Contains(svcBlock, "\n  - "+verb) {
				t.Fatalf("DPUService RBAC must not %s; VSP does not own Helm", verb)
			}
		}
	}
}

func TestClusterHandoffLiveRefusesWhenNoDPUService(t *testing.T) {
	cfg, err := restConfig()
	if err != nil {
		t.Skip("no kubeconfig")
	}
	scheme := newDPUServiceScheme(t)
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Skip(err.Error())
	}
	h := NewClusterHandoff(c)
	err = h.CreateIntentAccepted(context.Background(), validNF())
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(DPUServiceGVK.GroupVersion().WithKind("DPUServiceList"))
	if listErr := c.List(context.Background(), list); listErr != nil {
		if err == nil {
			t.Fatalf("list failed (%v) but create succeeded", listErr)
		}
		t.Logf("DPF not observable: %v", listErr)
		return
	}
	accepted := 0
	for i := range list.Items {
		if list.Items[i].GetDeletionTimestamp() != nil {
			continue
		}
		chart, _, _ := unstructured.NestedString(list.Items[i].Object, "spec", "helmChart", "source", "chart")
		if strings.TrimSpace(chart) != "" {
			accepted++
		}
	}
	if accepted == 0 && err == nil {
		t.Fatal("no accepted DPUService but CreateIntentAccepted succeeded")
	}
	if accepted > 0 && err != nil {
		t.Fatalf("live DPUService present but create refused: %v", err)
	}
	if del := h.DeleteIntentAccepted(context.Background(), validNF()); del != nil {
		t.Fatalf("detach must work once DPF is observable: %v", del)
	}
	t.Logf("live DPF intent accepted=%d createErr=%v", accepted, err)
}

func TestVSPDocStatesDPFHandoffContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "vsp.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, want := range []string{
		"DPUService",
		"do not Helm-install",
		"CreateNetworkFunction",
		"accepted",
		"networking-docs.nvidia.com/dpf/26.4.0/dpuservice-development-guide",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/vsp.md missing handoff contract %q", want)
		}
	}
}

func restConfig() (*rest.Config, error) {
	return ctrl.GetConfig()
}

func validNF() *pb.NFRequest {
	return &pb.NFRequest{Input: "00:00:00:00:00:01", Output: "00:00:00:00:00:02"}
}

func startServerWithHandoff(
	t *testing.T, h NetworkFunctionHandoff,
) (context.Context, *grpc.ClientConn, chan error, context.CancelFunc) {
	t.Helper()
	enum := discovery.MockEnumerator{
		Devices: []discovery.Device{
			discovery.StaticDevice(discovery.NVIDIAVendorID, "MTEXAMPLE0001", "0000:03:00.0", "BlueField-3"),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sock := fmt.Sprintf("%s/nvsp-nf-%d.sock", os.TempDir(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv := NewServer(enum, sock)
	srv.Handoff = h
	errc := make(chan error, 1)
	go func() { errc <- srv.Start(ctx) }()
	conn := waitDial(t, sock)
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, conn, errc, cancel
}

func newDPUServiceScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(DPUServiceGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(DPUServiceGVK.GroupVersion().WithKind("DPUServiceList"), &unstructured.UnstructuredList{})
	metav1.AddToGroupVersion(s, DPUServiceGVK.GroupVersion())
	return s
}

func acceptedDPUService(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(DPUServiceGVK)
	u.SetName(name)
	u.SetNamespace("dpf-operator-system")
	_ = unstructured.SetNestedField(u.Object, name, "spec", "serviceID")
	_ = unstructured.SetNestedField(
		u.Object, "https://helm.ngc.nvidia.com/nvidia/doca", "spec", "helmChart", "source", "repoURL",
	)
	_ = unstructured.SetNestedField(u.Object, "doca-hbn", "spec", "helmChart", "source", "chart")
	_ = unstructured.SetNestedField(u.Object, "v26.4.0", "spec", "helmChart", "source", "version")
	return u
}
