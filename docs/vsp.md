# Hardware discovery VSP
#
# Intel and Marvell VSPs in openshift/dpu-operator are gRPC servers the
# in-tree daemon dials over a unix socket. They identify a DPU by PCI
# vendor/device ID and serial (intel-netsec GetDpuPcieAddress reads the
# serial from the device and matches InitRequest.DpuIdentifier).
#
# This companion repo's translation engine does not speak that gRPC
# surface. It reads annotations on the OPI DataProcessingUnit:
#
#   provisioning.dpu.nvidia.com/serial-number
#   dpu.nvidia.com/bfb-url
#
# cmd/vsp is the missing link. It enumerates hardware and patches those
# annotations. MockEnumerator is for kind. PCIEnumerator scans
# /sys/bus/pci/devices for vendor 0x15b3 (function 0) and reads serial
# from sysfs `serial`, VPD keyword SN, or the PCIe Device Serial Number
# extended capability. The FieldMapping YAML then emits DPF objects with
# a real serialNumber — no Go if/else in the translator.
#
# On a worker with a BlueField:
#
#   lspci -nn | grep 15b3
#   ls /sys/bus/pci/devices/<bdf>/
#   cat /sys/bus/pci/devices/<bdf>/vendor
#   cat /sys/bus/pci/devices/<bdf>/serial   # if present
#   xxd /sys/bus/pci/devices/<bdf>/vpd      # SN keyword
#
#   # Day 1 (no cluster): bind the real daemon socket and scan PCI.
#   # --pci without --grpc-only calls GetConfigOrDie and needs kubeconfig.
#   sudo ./vsp --pci --grpc-only --node-name="$(hostname)"
#
#   # Only function 0 is enumerated. serial is often missing; VPD SN or
#   # the PCIe DSN in config is the fallback. Root is required for
#   # /var/run and usually for config-space reads.
#
# The same process serves LifeCycle/GetDevices on
# /var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock so the in-tree
# dpu-operator daemon can dial it.
#
# Socket share: kind uses emptyDir plus an opi-daemon sidecar
# (config/vsp/daemonset.yaml). An independently deployed daemon cannot
# see emptyDir. Hardware uses hostPath /var/run/dpu-daemon
# DirectoryOrCreate (config/overlays/hardware-vsp) and the official
# Bidirectional mount in config/opi-daemon. cmd/opi-daemon is the
# GrpcPlugin Init/GetDevices client.
#
# One unix socket multiplexes:
#   - opi_api.lifecycle.v1alpha1.LifeCycleService / DeviceService
#     (what internal/daemon/plugin GrpcPlugin currently dials)
#   - Vendor.LifeCycleService / DeviceService / NetworkFunctionService
#     (dpu-api)
#
# Selected contract (delegate-to-translator/DPF):
#   Official DPF v26.4 creates the workload from a DPUService Helm chart
#   (https://networking-docs.nvidia.com/dpf/26.4.0/dpuservice-development-guide).
#   This adapter's translator server-side-applies that DPUService from a
#   chart-bearing ServiceFunctionChain (config/mappings/servicefunctionchain.yaml).
#   Vendor.NetworkFunctionService Create/Delete stay thin: they do not Helm-install,
#   Helm-uninstall, or mutate DPF objects.
#
#   CreateNetworkFunction returns success only after DPF has accepted a
#   live DPUService (exists, not deleting, helmChart.source.chart set).
#   DeleteNetworkFunction is CNI detach. It succeeds only after DPF is
#   observable; it never reports that a workload was deleted. DPF still
#   owns the Helm release until the ServiceFunctionChain is removed.
#
#   grpc-only (no cluster client) uses RejectHandoff: Create/Delete fail
#   so a socket check cannot look like DPF accepted an intent.
#
# Stubs are imported; this repo does not run protoc. The lifecycle Go
# module path is github.com/opiproject/opi-api/v1/gen/go/lifecycle with a
# replace to github.com/bn222/opi-api — the same commit the daemon uses.
#
# Local socket check without a cluster (macOS cannot bind /var/run).
# Mock serial is MTEXAMPLE0001 at PCI 0000:03:00.0.
#
#   go run ./cmd/vsp --grpc-only --serial-number=MTEXAMPLE0001 \
#     --grpc-socket=/tmp/vendor-plugin.sock
#   grpcurl -plaintext unix:///tmp/vendor-plugin.sock list
#   grpcurl -plaintext -d '{}' unix:///tmp/vendor-plugin.sock \
#     Vendor.DeviceService/GetDevices
#   grpcurl -plaintext -d '{}' unix:///tmp/vendor-plugin.sock \
#     opi_api.lifecycle.v1alpha1.DeviceService/GetDevices
#   grpcurl -plaintext -d '{"dpuMode":false}' unix:///tmp/vendor-plugin.sock \
#     Vendor.LifeCycleService/Init
#   grpcurl -plaintext -d '{"dpuMode":true}' unix:///tmp/vendor-plugin.sock \
#     opi_api.lifecycle.v1alpha1.LifeCycleService/Init
#
# Expected list (order may vary):
#   Vendor.DeviceService
#   Vendor.HeartbeatService
#   Vendor.LifeCycleService
#   Vendor.NetworkFunctionService
#   opi_api.lifecycle.v1alpha1.DeviceService
#   opi_api.lifecycle.v1alpha1.LifeCycleService
#
# Expected GetDevices (both APIs):
#   {"devices":{"0000:03:00.0":{"ID":"MTEXAMPLE0001","health":"Healthy"}}}
#
# Expected Init (both APIs): {"ip":"127.0.0.1","port":50051}

