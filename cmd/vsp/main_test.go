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

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	opi "github.com/opiproject/opi-api/v1/gen/go/lifecycle/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGRPCOnlyServesMockSerial(t *testing.T) {
	// macOS unix-socket paths cap at ~104 bytes; t.TempDir() overshoots.
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("vsp-grpc-%d.sock", time.Now().UnixNano()))
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("vsp-bin-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(sock)
		_ = os.Remove(bin)
	})
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin,
		"--grpc-only",
		"--serial-number=MTEXAMPLE0001",
		"--grpc-socket="+sock,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProcess(t, cmd) })

	conn := waitUnix(t, sock)
	t.Cleanup(func() { _ = conn.Close() })
	got, err := pb.NewDeviceServiceClient(conn).GetDevices(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	dev := got.GetDevices()["0000:03:00.0"]
	if dev == nil || dev.GetID() != "MTEXAMPLE0001" {
		t.Fatalf("vendor inventory = %v", got.GetDevices())
	}
	opiDevs, err := opi.NewDeviceServiceClient(conn).GetDevices(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if opiDevs.GetDevices()["0000:03:00.0"].GetID() != "MTEXAMPLE0001" {
		t.Fatalf("opi inventory = %v", opiDevs.GetDevices())
	}
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func waitUnix(t *testing.T, sock string) *grpc.ClientConn {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		raw, err := net.DialTimeout("unix", sock, 100*time.Millisecond)
		if err != nil {
			last = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = raw.Close()
		conn, err := grpc.NewClient("passthrough:///"+sock,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	t.Fatalf("vsp did not listen on %s: %v", sock, last)
	return nil
}

// The daemonset sets DPU_BFB_URL and the README documents --bfb-url. Both
// resolve through the deprecated alias, so losing this fallback would stop
// the firmware URL being stamped without any error.
func TestFirstNonEmptyKeepsDeprecatedAliasWorking(t *testing.T) {
	for _, tc := range []struct {
		name                string
		preferred, fallback string
		want                string
	}{
		{"new name only", "https://new", "", "https://new"},
		{"deprecated alias only", "", "https://old", "https://old"},
		{"new name wins", "https://new", "https://old", "https://new"},
		{"neither set", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstNonEmpty(tc.preferred, tc.fallback); got != tc.want {
				t.Errorf("firstNonEmpty(%q, %q) = %q, want %q",
					tc.preferred, tc.fallback, got, tc.want)
			}
		})
	}
}
