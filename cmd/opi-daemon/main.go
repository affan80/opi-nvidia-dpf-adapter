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

// OPI daemon handshake client. Official dpu-operator GrpcPlugin dials
// /var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock and calls
// LifeCycle.Init then Device.GetDevices. This binary is that first
// contact so a sidecar or an independently deployed DaemonSet can prove
// the VSP socket is reachable without installing the full CNI/device-plugin
// daemon.
package main

import (
	"flag"
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/vsp"
)

var setupLog = ctrl.Log.WithName("opi-daemon")

func main() {
	var (
		grpcSocket string
		identifier string
		dpuMode    bool
		once       bool
	)
	flag.StringVar(&grpcSocket, "grpc-socket", vsp.DefaultSocket,
		"unix socket the official GrpcPlugin dials")
	flag.StringVar(&identifier, "dpu-identifier", os.Getenv("DPU_IDENTIFIER"),
		"InitRequest.DpuIdentifier (serial)")
	flag.BoolVar(&dpuMode, "dpu-mode", true, "InitRequest.DpuMode")
	flag.BoolVar(&once, "once", false, "exit after one successful handshake")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := ctrl.SetupSignalHandler()

	var (
		got *vsp.HandshakeResult
		err error
	)
	if once {
		got, err = vsp.Handshake(ctx, grpcSocket, dpuMode, identifier)
	} else {
		got, err = vsp.HandshakeUntil(ctx, grpcSocket, dpuMode, identifier)
	}
	if err != nil {
		setupLog.Error(err, "OPI daemon could not reach VSP")
		os.Exit(1)
	}
	setupLog.Info("Init/GetDevices ok",
		"ip", got.IP,
		"port", got.Port,
		"devices", fmt.Sprintf("%d", len(got.Devices)),
		"socket", grpcSocket,
	)
	for pci, d := range got.Devices {
		setupLog.Info("device", "pci", pci, "id", d.ID, "health", d.Health)
	}
	if once {
		return
	}
	<-ctx.Done()
}
