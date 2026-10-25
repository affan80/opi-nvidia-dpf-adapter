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
	"testing"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	opi "github.com/opiproject/opi-api/v1/gen/go/lifecycle/v1alpha1"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/discovery"
)

func TestGetDevicesDropsGoneAndInvalidDevices(t *testing.T) {
	enum := &discovery.MutableEnumerator{Devices: []discovery.Device{
		discovery.StaticDevice(discovery.NVIDIAVendorID, "MTA0001", "0000:0f:00.0", "BlueField-2"),
		discovery.StaticDevice(discovery.NVIDIAVendorID, "MTB0002", "0000:1b:00.0", "BlueField-2"),
		{PCIAddress: "0000:1c:00.0", VendorID: discovery.NVIDIAVendorID},
	}}
	ctx, conn, errc, cancel := startServer(t, enum)

	got, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetDevices()) != 2 || got.GetDevices()["0000:1c:00.0"] != nil {
		t.Fatalf("multi-device inventory leaked an incomplete identity: %v", got.GetDevices())
	}

	enum.Set([]discovery.Device{
		discovery.StaticDevice(discovery.NVIDIAVendorID, "MTB0002", "0000:1b:00.0", "BlueField-2"),
	}, nil)
	got, err = pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetDevices()["0000:0f:00.0"] != nil {
		t.Fatalf("removed card is a false-positive: %v", got.GetDevices())
	}
	if got.GetDevices()["0000:1b:00.0"].GetID() != "MTB0002" {
		t.Fatalf("remaining card: %v", got.GetDevices())
	}

	enum.Set(nil, nil)
	got, err = pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetDevices()) != 0 {
		t.Fatalf("empty bus after reboot/remove: %v", got.GetDevices())
	}

	waitStop(t, cancel, errc)
}

func TestGetDevicesLiveBlueFieldRefresh(t *testing.T) {
	live, err := discovery.LiveDevices(discovery.PCIEnumerator{})
	if err != nil || len(live) == 0 {
		t.Skip("no live BlueField inventory")
	}
	ctx, conn, errc, cancel := startServer(t, discovery.PCIEnumerator{})
	got, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetDevices()) != len(live) {
		t.Fatalf("GetDevices=%v live=%v", got.GetDevices(), live)
	}
	for _, d := range live {
		if got.GetDevices()[d.PCIAddress].GetID() != d.SerialNumber {
			t.Fatalf("%s: GetDevices=%v", d.PCIAddress, got.GetDevices())
		}
	}
	again, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.GetDevices()) != len(got.GetDevices()) {
		t.Fatalf("refresh changed inventory %v -> %v", got.GetDevices(), again.GetDevices())
	}
	t.Logf("live GetDevices %d device(s)", len(got.GetDevices()))
	waitStop(t, cancel, errc)
}

func TestGetDevicesRestartRescansAndBothAPIsAgree(t *testing.T) {
	enum := &discovery.MutableEnumerator{Devices: []discovery.Device{
		discovery.StaticDevice(discovery.NVIDIAVendorID, "MTEXAMPLE0001", "0000:03:00.0", "BlueField-3"),
	}}
	ctx, conn, errc, cancel := startServer(t, enum)
	vendor, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	waitStop(t, cancel, errc)

	enum.Set([]discovery.Device{
		discovery.StaticDevice(discovery.NVIDIAVendorID, "MTEXAMPLE0001", "0000:0f:00.0", "BlueField-3"),
	}, nil)
	ctx, conn, errc, cancel = startServer(t, enum)
	again, err := pb.NewDeviceServiceClient(conn).GetDevices(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	opiDevs, err := opi.NewDeviceServiceClient(conn).GetDevices(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if vendor.GetDevices()["0000:03:00.0"] == nil {
		t.Fatal("pre-restart inventory")
	}
	if again.GetDevices()["0000:03:00.0"] != nil || again.GetDevices()["0000:0f:00.0"].GetID() != "MTEXAMPLE0001" {
		t.Fatalf("restart must not keep the old PCI: %v", again.GetDevices())
	}
	if opiDevs.GetDevices()["0000:0f:00.0"].GetID() != again.GetDevices()["0000:0f:00.0"].GetID() {
		t.Fatalf("APIs diverged after restart vendor=%v opi=%v", again.GetDevices(), opiDevs.GetDevices())
	}
	waitStop(t, cancel, errc)
}
