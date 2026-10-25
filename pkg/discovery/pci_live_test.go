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
	"os"
	"path/filepath"
	"testing"
)

// TestPCIEnumeratorLiveBlueFieldIdentities is the hardware gate. On a
// worker with a 0x15b3 function 0 it must return a real PCI address and
// serial, once per board. CI/kind skip because they have no BlueField.
func TestPCIEnumeratorLiveBlueFieldIdentities(t *testing.T) {
	if _, err := os.Stat(defaultSysfsPCI); err != nil {
		t.Skip("no PCI sysfs")
	}
	if !liveNVIDIAPresent(t) {
		t.Skip("no BlueField (0x15b3) on this bus")
	}

	got, err := PCIEnumerator{}.Enumerate()
	if err != nil {
		t.Fatalf("enumerate live BlueField: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("BlueField is on the bus but enumerator returned no identity; provisioning would invent a serial")
	}

	seen := map[string]string{}
	nvidia := 0
	for _, d := range got {
		if d.VendorID != NVIDIAVendorID {
			continue
		}
		nvidia++
		if err := ValidateIdentity(d); err != nil {
			t.Errorf("%s: %v", d.PCIAddress, err)
		}
		if pciFunction(d.PCIAddress) != "0" {
			t.Errorf("non-function-0 %s", d.PCIAddress)
		}
		if _, ok := supportedVendors[NVIDIAVendorID].products[d.DeviceID]; !ok {
			t.Errorf("%s device %#x is not a supported BlueField PF", d.PCIAddress, d.DeviceID)
		}
		if prev, ok := seen[d.SerialNumber]; ok {
			t.Errorf("duplicate serial %s on %s and %s", d.SerialNumber, prev, d.PCIAddress)
		}
		seen[d.SerialNumber] = d.PCIAddress
		t.Logf("live identity pci=%s device=%#x product=%s serial=%s",
			d.PCIAddress, d.DeviceID, d.ProductName, d.SerialNumber)
	}
	if nvidia == 0 {
		t.Fatal("enumerator returned devices but none were NVIDIA")
	}

	again, err := LiveDevices(PCIEnumerator{})
	if err != nil {
		t.Fatalf("refresh live inventory: %v", err)
	}
	if len(again) != nvidia {
		t.Fatalf("idempotent refresh changed count %d -> %d", nvidia, len(again))
	}
	for _, d := range again {
		if d.VendorID != NVIDIAVendorID {
			continue
		}
		if prev := seen[d.SerialNumber]; prev != d.PCIAddress {
			t.Errorf("refresh moved serial %s from %s to %s", d.SerialNumber, prev, d.PCIAddress)
		}
	}
}

func liveNVIDIAPresent(t *testing.T) bool {
	t.Helper()
	entries, err := os.ReadDir(defaultSysfsPCI)
	if err != nil {
		return false
	}
	for _, e := range entries {
		v, err := readPCIID(filepath.Join(defaultSysfsPCI, e.Name(), "vendor"))
		if err == nil && v == NVIDIAVendorID && pciFunction(e.Name()) == "0" {
			id, _ := readPCIID(filepath.Join(defaultSysfsPCI, e.Name(), "device"))
			if supportedVendors[NVIDIAVendorID].productName(id) != "" {
				return true
			}
		}
	}
	return false
}
