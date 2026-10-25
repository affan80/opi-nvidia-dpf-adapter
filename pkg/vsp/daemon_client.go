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
	"fmt"
	"net"
	"time"

	opi "github.com/opiproject/opi-api/v1/gen/go/lifecycle/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// HandshakeDevice is one GetDevices entry the official GrpcPlugin consumes.
type HandshakeDevice struct {
	ID     string
	Health string
}

// HandshakeResult is the official daemon's first two RPCs: Init then GetDevices.
type HandshakeResult struct {
	IP      string
	Port    int32
	Devices map[string]*HandshakeDevice
}

// Handshake dials the VSP unix socket the way GrpcPlugin does and calls
// opi-api LifeCycle.Init plus Device.GetDevices.
func Handshake(ctx context.Context, socket string, dpuMode bool, identifier string) (*HandshakeResult, error) {
	if socket == "" {
		socket = DefaultSocket
	}
	conn, err := grpc.NewClient("passthrough:///"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	defer func() { _ = conn.Close() }()

	ip, err := opi.NewLifeCycleServiceClient(conn).Init(ctx, &opi.InitRequest{
		DpuMode:       dpuMode,
		DpuIdentifier: identifier,
	})
	if err != nil {
		return nil, fmt.Errorf("Init: %w", err)
	}
	got, err := opi.NewDeviceServiceClient(conn).GetDevices(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("GetDevices: %w", err)
	}
	devices := make(map[string]*HandshakeDevice, len(got.GetDevices()))
	for k, d := range got.GetDevices() {
		devices[k] = &HandshakeDevice{ID: d.GetID(), Health: d.GetHealth()}
	}
	return &HandshakeResult{
		IP:      ip.GetIp(),
		Port:    ip.GetPort(),
		Devices: devices,
	}, nil
}

// HandshakeUntil retries Handshake the way GrpcPlugin.Start waits for the
// VSP socket to appear (sidecar starts before the VSP listener, or an
// independently scheduled daemon races the VSP DaemonSet).
func HandshakeUntil(ctx context.Context, socket string, dpuMode bool, identifier string) (*HandshakeResult, error) {
	interval := 100 * time.Millisecond
	var last error
	for {
		got, err := Handshake(ctx, socket, dpuMode, identifier)
		if err == nil {
			return got, nil
		}
		last = err
		select {
		case <-ctx.Done():
			if last != nil {
				return nil, fmt.Errorf("handshake: %w", last)
			}
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
