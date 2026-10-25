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

package discovery

import (
	"errors"
	"testing"
)

func TestLiveDevicesDropsInvalidAndPropagatesEnumerateError(t *testing.T) {
	got, err := LiveDevices(MockEnumerator{Devices: []Device{
		StaticDevice(NVIDIAVendorID, testSerial, testPCI, testProduct),
		{PCIAddress: "0000:04:00.0", VendorID: NVIDIAVendorID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SerialNumber != testSerial {
		t.Fatalf("live=%v", got)
	}

	_, err = LiveDevices(failingEnum{err: errors.New("bus down")})
	if err == nil || err.Error() != "bus down" {
		t.Fatalf("enumerate error = %v", err)
	}
	_, err = LiveDevices(nil)
	if err == nil {
		t.Fatal("nil enumerator")
	}
}

func TestAssignDevicesIsIdempotentOnSameInventory(t *testing.T) {
	dev := StaticDevice(NVIDIAVendorID, testSerial, testPCI, testProduct)
	dpus := []DPUView{{
		Name: testDPUName,
		Annotations: map[string]string{
			SerialNumberAnnotation: testSerial,
			PCIAddressAnnotation:   DPFPCIAddress(testPCI),
		},
	}}
	a := AssignDevices(dpus, []Device{dev})
	b := AssignDevices(dpus, []Device{dev})
	if !a[testDPUName].Bound || a[testDPUName].Device.SerialNumber != testSerial {
		t.Fatalf("first=%+v", a[testDPUName])
	}
	if a[testDPUName].Device != b[testDPUName].Device || a[testDPUName].Clear != b[testDPUName].Clear {
		t.Fatalf("refresh must not move a bound DPU: %+v vs %+v", a[testDPUName], b[testDPUName])
	}
}

func TestAssignDevicesClearsStaleIdentityWhenHardwareGone(t *testing.T) {
	got := AssignDevices([]DPUView{{
		Name: testDPUName,
		Annotations: map[string]string{
			SerialNumberAnnotation: testSerial,
			PCIAddressAnnotation:   "0000-03-00",
			FlavorAnnotation:       testFlavor,
		},
	}}, nil)
	asg := got[testDPUName]
	if !asg.Clear || asg.Bound {
		t.Fatalf("disappeared card must clear, not bind: %+v", asg)
	}
}

func TestAssignDevicesDoesNotTreatStaleSerialAsLiveDevice(t *testing.T) {
	other := StaticDevice(NVIDIAVendorID, "MTOTHER0002", "0000:0b:00.0", testProduct)
	got := AssignDevices([]DPUView{{
		Name:        "dpu-a",
		Annotations: map[string]string{SerialNumberAnnotation: "MTGONE0001"},
	}, {
		Name:        "dpu-b",
		Annotations: map[string]string{SerialNumberAnnotation: "MTOTHER0002"},
	}}, []Device{other})
	if got["dpu-a"].Bound {
		t.Fatalf("dpu-a stole the other card: %+v", got["dpu-a"])
	}
	if !got["dpu-a"].Clear {
		t.Fatalf("dpu-a stale serial must clear: %+v", got["dpu-a"])
	}
	if !got["dpu-b"].Bound || got["dpu-b"].Device.SerialNumber != "MTOTHER0002" {
		t.Fatalf("dpu-b=%+v", got["dpu-b"])
	}
}

func TestAssignDevicesUpdatesPCIAfterRebootMove(t *testing.T) {
	moved := StaticDevice(NVIDIAVendorID, testSerial, "0000:0f:00.0", testProduct)
	got := AssignDevices([]DPUView{{
		Name: testDPUName,
		Annotations: map[string]string{
			SerialNumberAnnotation: testSerial,
			PCIAddressAnnotation:   "0000-03-00",
		},
	}}, []Device{moved})
	asg := got[testDPUName]
	if !asg.Bound || asg.Device.PCIAddress != "0000:0f:00.0" {
		t.Fatalf("same serial after reboot must follow new PCI: %+v", asg)
	}
}

func TestAssignDevicesReplacesSingleCardByPCI(t *testing.T) {
	replacement := StaticDevice(NVIDIAVendorID, "MTNEW0003", testPCI, testProduct)
	got := AssignDevices([]DPUView{{
		Name: testDPUName,
		Annotations: map[string]string{
			SerialNumberAnnotation: "MTOLD0001",
			PCIAddressAnnotation:   DPFPCIAddress(testPCI),
		},
	}}, []Device{replacement})
	asg := got[testDPUName]
	if !asg.Bound || asg.Device.SerialNumber != "MTNEW0003" {
		t.Fatalf("same PCI slot after card swap: %+v", asg)
	}
}

func TestAssignDevicesPairsAddedCardsOnMultiDeviceHost(t *testing.T) {
	a := StaticDevice(NVIDIAVendorID, "MTA0001", "0000:0f:00.0", testProduct)
	b := StaticDevice(NVIDIAVendorID, "MTB0002", "0000:1b:00.0", testProduct)
	got := AssignDevices([]DPUView{
		{Name: "dpu-a"},
		{Name: "dpu-b"},
	}, []Device{b, a})
	if !got["dpu-a"].Bound || got["dpu-a"].Device.SerialNumber != "MTA0001" {
		t.Fatalf("dpu-a (name order) should take lowest PCI: %+v", got["dpu-a"])
	}
	if !got["dpu-b"].Bound || got["dpu-b"].Device.SerialNumber != "MTB0002" {
		t.Fatalf("dpu-b=%+v", got["dpu-b"])
	}

	again := AssignDevices([]DPUView{
		{Name: "dpu-a", Annotations: mustIdentity(t, a)},
		{Name: "dpu-b", Annotations: mustIdentity(t, b)},
	}, []Device{a, b})
	if again["dpu-a"].Device.SerialNumber != "MTA0001" || again["dpu-b"].Device.SerialNumber != "MTB0002" {
		t.Fatalf("refresh swapped devices: %+v", again)
	}
}

func TestAssignDevicesDoesNotGuessWhenTwoStaleDPUsShareOneCard(t *testing.T) {
	live := StaticDevice(NVIDIAVendorID, "MTLIVE", "0000:0f:00.0", testProduct)
	got := AssignDevices([]DPUView{{
		Name:        "dpu-a",
		Annotations: map[string]string{SerialNumberAnnotation: "MTGONEA"},
	}, {
		Name:        "dpu-b",
		Annotations: map[string]string{SerialNumberAnnotation: "MTGONEB"},
	}}, []Device{live})
	if got["dpu-a"].Bound || got["dpu-b"].Bound {
		t.Fatalf("must not invent which stale DPU owns the leftover card: %+v", got)
	}
	if !got["dpu-a"].Clear || !got["dpu-b"].Clear {
		t.Fatalf("both stale stamps must clear: %+v", got)
	}
}

func TestStripIdentityLeavesFirmware(t *testing.T) {
	ann := map[string]string{
		SerialNumberAnnotation: testSerial,
		PCIAddressAnnotation:   "0000-03-00",
		FlavorAnnotation:       testFlavor,
		BFBURLAnnotation:       testBFBURL,
	}
	StripIdentity(ann)
	if HasIdentity(ann) {
		t.Fatalf("identity left: %v", ann)
	}
	if ann[FlavorAnnotation] != testFlavor || ann[BFBURLAnnotation] != testBFBURL {
		t.Fatalf("firmware is not inventory, got %v", ann)
	}
}

type failingEnum struct{ err error }

func (f failingEnum) Enumerate() ([]Device, error) { return nil, f.err }

func mustIdentity(t *testing.T, d Device) map[string]string {
	t.Helper()
	ann, err := IdentityAnnotations(d)
	if err != nil {
		t.Fatal(err)
	}
	return ann
}
