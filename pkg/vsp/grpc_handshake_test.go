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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	opi "github.com/opiproject/opi-api/v1/gen/go/lifecycle/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/discovery"
)

// Services grpcurl list must show on the multiplexed unix socket.
// OPI Heartbeat is not registered: GrpcPlugin dials LifeCycle/Device only.
var requiredServices = []string{
	pb.LifeCycleService_ServiceDesc.ServiceName,
	pb.DeviceService_ServiceDesc.ServiceName,
	pb.NetworkFunctionService_ServiceDesc.ServiceName,
	pb.HeartbeatService_ServiceDesc.ServiceName,
	opi.LifeCycleService_ServiceDesc.ServiceName,
	opi.DeviceService_ServiceDesc.ServiceName,
}

func TestReflectionListsBothHandshakes(t *testing.T) {
	ctx, conn, errc, cancel := startTestServer(t)
	got := listReflectedServices(t, ctx, conn)
	for _, name := range requiredServices {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %s; have %v", name, keys(got))
		}
	}
	waitStop(t, cancel, errc)
}

func TestGetDevicesRequiresEnumerator(t *testing.T) {
	ctx, conn, errc, cancel := startServer(t, nil)
	_, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err == nil || !strings.Contains(err.Error(), "no hardware enumerator") {
		t.Fatalf("nil enumerator = %v", err)
	}
	waitStop(t, cancel, errc)
}

func TestOpiGetDevicesPropagatesEnumerateError(t *testing.T) {
	ctx, conn, errc, cancel := startServer(t, failingEnumerator{err: errors.New("bus down")})
	_, err := opi.NewDeviceServiceClient(conn).GetDevices(ctx, &emptypb.Empty{})
	if err == nil || !strings.Contains(err.Error(), "bus down") {
		t.Fatalf("opi GetDevices = %v", err)
	}
	waitStop(t, cancel, errc)
}

func TestInitAndGetDevicesMatchOnBothAPIs(t *testing.T) {
	ctx, conn, errc, cancel := startTestServer(t)

	vendorIP, err := pb.NewLifeCycleServiceClient(conn).Init(ctx, &pb.InitRequest{
		DpuMode: false, DpuIdentifier: "MTEXAMPLE0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	opiIP, err := opi.NewLifeCycleServiceClient(conn).Init(ctx, &opi.InitRequest{
		DpuMode: true, DpuIdentifier: "MTEXAMPLE0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if vendorIP.GetIp() != opiIP.GetIp() || vendorIP.GetPort() != opiIP.GetPort() {
		t.Fatalf("Init disagree vendor=%+v opi=%+v", vendorIP, opiIP)
	}

	vendor, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	opiDevs, err := opi.NewDeviceServiceClient(conn).GetDevices(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(vendor.GetDevices()) != 1 || len(opiDevs.GetDevices()) != 1 {
		t.Fatalf("inventory vendor=%v opi=%v", vendor.GetDevices(), opiDevs.GetDevices())
	}
	v := vendor.GetDevices()["0000:03:00.0"]
	o := opiDevs.GetDevices()["0000:03:00.0"]
	if v == nil || o == nil || v.GetID() != "MTEXAMPLE0001" || o.GetID() != v.GetID() || o.GetHealth() != v.GetHealth() {
		t.Fatalf("device vendor=%+v opi=%+v", v, o)
	}

	waitStop(t, cancel, errc)
}

func TestGrpcurlListsServicesAndMockInventory(t *testing.T) {
	exe, err := exec.LookPath("grpcurl")
	if err != nil {
		t.Skip("grpcurl not on PATH")
	}
	sock := fmt.Sprintf("%s/grpcurl-%d.sock", os.TempDir(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(sock) })
	enum := discovery.MockEnumerator{
		Devices: []discovery.Device{
			discovery.StaticDevice(discovery.NVIDIAVendorID, "MTEXAMPLE0001", "0000:03:00.0", "BlueField-3"),
		},
	}
	srv := NewServer(enum, sock)
	serveCtx, serveCancel := context.WithCancel(context.Background())
	t.Cleanup(serveCancel)
	errc := make(chan error, 1)
	go func() { errc <- srv.Start(serveCtx) }()
	_ = waitDial(t, sock)

	listed := runGrpcurl(t, exe, sock, nil, "list")
	for _, name := range requiredServices {
		if !strings.Contains(listed, name) {
			t.Errorf("grpcurl list missing %s\n%s", name, listed)
		}
	}

	vendorDevs := runGrpcurl(t, exe, sock, []string{"-d", "{}"}, "Vendor.DeviceService/GetDevices")
	if !strings.Contains(vendorDevs, "MTEXAMPLE0001") || !strings.Contains(vendorDevs, "0000:03:00.0") {
		t.Errorf("Vendor.GetDevices inventory:\n%s", vendorDevs)
	}
	opiDevs := runGrpcurl(t, exe, sock, []string{"-d", "{}"}, "opi_api.lifecycle.v1alpha1.DeviceService/GetDevices")
	if !strings.Contains(opiDevs, "MTEXAMPLE0001") || !strings.Contains(opiDevs, "0000:03:00.0") {
		t.Errorf("OPI GetDevices inventory:\n%s", opiDevs)
	}
	vendorInit := runGrpcurl(t, exe, sock, []string{"-d", `{"dpuMode":false}`}, "Vendor.LifeCycleService/Init")
	if !strings.Contains(vendorInit, "127.0.0.1") || !strings.Contains(vendorInit, "50051") {
		t.Errorf("Vendor.Init:\n%s", vendorInit)
	}
	opiInit := runGrpcurl(t, exe, sock,
		[]string{"-d", `{"dpuMode":true}`},
		"opi_api.lifecycle.v1alpha1.LifeCycleService/Init")
	if !strings.Contains(opiInit, "127.0.0.1") || !strings.Contains(opiInit, "50051") {
		t.Errorf("OPI Init:\n%s", opiInit)
	}

	serveCancel()
	select {
	case <-errc:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

type failingEnumerator struct{ err error }

func (f failingEnumerator) Enumerate() ([]discovery.Device, error) {
	return nil, f.err
}

func startServer(
	t *testing.T, enum discovery.Enumerator,
) (context.Context, *grpc.ClientConn, chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sock := fmt.Sprintf("%s/nvsp-%d.sock", os.TempDir(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv := NewServer(enum, sock)
	errc := make(chan error, 1)
	go func() { errc <- srv.Start(ctx) }()
	conn := waitDial(t, sock)
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, conn, errc, cancel
}

func listReflectedServices(t *testing.T, ctx context.Context, conn *grpc.ClientConn) map[string]struct{} {
	t.Helper()
	stream, err := grpc_reflection_v1.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{
			ListServices: "*",
		},
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	list := resp.GetListServicesResponse()
	if list == nil {
		t.Fatalf("reflection list: %v", resp)
	}
	out := map[string]struct{}{}
	for _, svc := range list.GetService() {
		out[svc.GetName()] = struct{}{}
	}
	return out
}

func runGrpcurl(t *testing.T, exe, sock string, flags []string, symbol string) string {
	t.Helper()
	cmdArgs := append([]string{"-plaintext"}, flags...)
	cmdArgs = append(cmdArgs, "unix://"+sock, symbol)
	out, err := exec.Command(exe, cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("grpcurl %v %s: %v\n%s", flags, symbol, err, out)
	}
	return string(out)
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
