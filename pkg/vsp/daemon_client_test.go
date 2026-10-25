/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    10|Unless required by applicable law or agreed to in writing, software
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
	"strings"
	"testing"
	"time"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/discovery"
)

func TestHandshakeCallsOpiInitAndGetDevices(t *testing.T) {
	ctx, sock, errc, cancel := startClientServer(t, discovery.MockEnumerator{
		Devices: []discovery.Device{
			discovery.StaticDevice(discovery.NVIDIAVendorID, "MTEXAMPLE0001", "0000:03:00.0", "BlueField-3"),
		},
	})

	got, err := Handshake(ctx, sock, true, "MTEXAMPLE0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != defaultIP || got.Port != defaultPort {
		t.Fatalf("Init = %s:%d", got.IP, got.Port)
	}
	dev := got.Devices["0000:03:00.0"]
	if dev == nil || dev.ID != "MTEXAMPLE0001" || dev.Health != healthOK {
		t.Fatalf("GetDevices = %+v", got.Devices)
	}
	waitStop(t, cancel, errc)
}

func TestHandshakeRequiresReachableSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Handshake(ctx, "/tmp/opi-daemon-missing.sock", false, "")
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestHandshakePropagatesGetDevicesError(t *testing.T) {
	ctx, sock, errc, cancel := startClientServer(t, failingEnumerator{err: errors.New("bus down")})
	_, err := Handshake(ctx, sock, false, "")
	if err == nil || !strings.Contains(err.Error(), "bus down") {
		t.Fatalf("Handshake = %v", err)
	}
	waitStop(t, cancel, errc)
}

func startClientServer(
	t *testing.T, enum discovery.Enumerator,
) (context.Context, string, chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sock := fmt.Sprintf("%s/nvsp-hs-%d.sock", os.TempDir(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(sock) })
	errc := make(chan error, 1)
	go func() { errc <- NewServer(enum, sock).Start(ctx) }()
	_ = waitDial(t, sock)
	return ctx, sock, errc, cancel
}
