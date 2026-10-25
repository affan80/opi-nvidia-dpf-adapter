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

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnceReachesVSPInitAndGetDevices(t *testing.T) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("opi-hs-%d.sock", time.Now().UnixNano()))
	vspBin := filepath.Join(os.TempDir(), fmt.Sprintf("vsp-hs-%d", time.Now().UnixNano()))
	daemonBin := filepath.Join(os.TempDir(), fmt.Sprintf("opi-hs-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(sock)
		_ = os.Remove(vspBin)
		_ = os.Remove(daemonBin)
	})

	vspSrc := filepath.Join("..", "vsp")
	if out, err := exec.Command("go", "build", "-o", vspBin, vspSrc).CombinedOutput(); err != nil {
		t.Fatalf("go build vsp: %v\n%s", err, out)
	}
	if out, err := exec.Command("go", "build", "-o", daemonBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build opi-daemon: %v\n%s", err, out)
	}

	vsp := exec.Command(vspBin,
		"--grpc-only",
		"--serial-number=MTEXAMPLE0001",
		"--grpc-socket="+sock,
	)
	vsp.Stderr = os.Stderr
	if err := vsp.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProcess(t, vsp) })
	waitUnixFile(t, sock)

	cmd := exec.Command(daemonBin, "--once", "--grpc-socket="+sock, "--dpu-identifier=MTEXAMPLE0001")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opi-daemon --once: %v\n%s", err, out)
	}
	log := string(out)
	if !strings.Contains(log, "Init/GetDevices ok") {
		t.Fatalf("missing handshake success:\n%s", log)
	}
	if !strings.Contains(log, "MTEXAMPLE0001") {
		t.Fatalf("missing serial:\n%s", log)
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

func waitUnixFile(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		raw, err := net.DialTimeout("unix", sock, 100*time.Millisecond)
		if err == nil {
			_ = raw.Close()
			return
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("vsp did not listen on %s: %v", sock, last)
}
