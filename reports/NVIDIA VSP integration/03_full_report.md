# NVIDIA VSP integration — full report

Date: 2026-10-25. Author context: OPI lab on dh2 through the VPN Mac. Repo `opi-nvidia-dpf-adapter`, branch `LifecycleManager`. This report covers the hardware VSP work: socket share, PCI identity, inventory across reboot and multi-device hosts, network-function handoff, the live BlueField proof, and the reboot that is still open. Changes are uncommitted.

## 1. Scope

The in-tree dpu-operator daemon dials a vendor plugin on a unix socket. It calls LifeCycle `Init` and DeviceService `GetDevices`, and it can call NetworkFunction Create and Delete. This adapter's translator does not speak that socket. It reads annotations on `DataProcessingUnit` and server-side-applies DPF objects.

`cmd/vsp` is the plugin. It scans PCI, stamps annotations, and serves both gRPC stacks on one socket:

- `opi_api.lifecycle.v1alpha1` LifeCycleService and DeviceService
- `Vendor` LifeCycleService, DeviceService, HeartbeatService, and NetworkFunctionService

The translator remains the writer of DPF objects. The VSP does not Helm-install DPF and does not mutate a DPUService.

## 2. Pins

| Item | Value |
|---|---|
| Kubernetes | v1.36.4 |
| k3s on dh2 | v1.36.4+k3s1 |
| controller-runtime | v0.24.1 |
| DPF | v26.4.0, window [v26.4.0, v27.0.0) |
| Go toolchain used for lab binaries | go1.26.10 |
| OPI mappings | ConfigMap `opi-nvidia-dpf-adapter-mappings`, mounted at `/mappings` |
| Vendor enum | `nvidia` or `amd` |
| Helm library | not imported |

`cmd/main.go` loads mappings once at start. A ConfigMap edit needs a manager restart. That restart was done on 2026-10-25 after the nvconfig CEL change.

## 3. Hardware found

| Field | Value |
|---|---|
| Host | dh2 `172.22.1.2` |
| PCI | `0000:0f:00.0` (`0000-0f-00` in DPF) |
| Vendor / device | `0x15b3:0xa2d6` BlueField-2 MT42822 |
| Second PF | `0000:0f:00.1`, same serial, not a second DPU |
| SoC management | `0000:0f:00.2` `0xc2d3`, skipped |
| Serial | `MT2321XZ0KVX` from VPD / DSN. sysfs `serial` is absent |
| OPI object | `DataProcessingUnit/dh2-bluefield-2` |
| DPF device | `DPUDevice/mt2321xz0kvx` Ready, Discovered, NodeAttached |
| DPF DPU | `DPU/dh2-bluefield-2` |
| BFB | `bf-bundle-3.4.0-92` Ubuntu 24.04 |
| VSP pod | `opi-nvidia-dpf-adapter-nvidia-vsp-bfhws` 1/1 |
| Socket | `vendor-plugin.sock` |

The socket file is `/var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock`. Handshake at 2026-10-25 08:54:11Z: Init identifier `MT2321XZ0KVX`, GetDevices count 1, health Healthy.

## 4. What the code does now

Discovery (`pkg/discovery`): function 0 only, NVIDIA products BF2 `0xa2d2`/`0xa2d6` and BF3 `0xa2dc`/`0xa2dd`. One device per serial. Identity requires PCI and serial. Inventory refresh is 15 seconds. A failed scan does not wipe stamps. A successful empty scan clears identity keys only.

Annotator writes `provisioning.dpu.nvidia.com/serial-number`, `provisioning.dpu.nvidia.com/pci-address`, and the firmware annotations. It lists DPUs for the node and requeues.

VSP (`pkg/vsp`, `cmd/vsp`, `cmd/opi-daemon`): GetDevices calls `LiveDevices` on every RPC. In-cluster handoff reads DPUServices. grpc-only rejects Create and Delete. Kind shares the socket with a sidecar. Hardware uses the host path and no sidecar.

Mapping: `spec.serialNumber` and `spec.pciAddress` are required. Flavor nvconfig follows official HBN defaults unless annotations say otherwise. Scale-function `0` still means 20 and 10. The other reset-needed keys keep an explicit 0. Those overrides apply only when the flavor is created. They cannot edit `dpf-default-flavor` while `dh2-bluefield-2` points at it.

## 5. Test results

Local `make test` evidence (`evidence/unit-tests.log`):

| Package | Result | Coverage |
|---|---|---|
| cmd/vsp | ok 2.648s | 5.5% |
| pkg/discovery | ok 0.775s | 80.4% |
| pkg/lifecycle | ok 2.015s | 89.2% |
| pkg/mapping | ok 1.369s | 80.3% |
| pkg/translation | ok 7.153s | 72.7% |
| pkg/vsp | ok 4.665s | 73.0% |
| test/conformance | ok 11.413s | envtest |

After the nvconfig CEL edit, `go test ./pkg/mapping/ ./pkg/translation/ -run 'Flavor|Nvconfig|DPUReady|ReconcileDPUFlavor'` passed (mapping about 0.8s, translation about 0.9s). The full suite was not re-run after that edit.

Lab: live PCI test and a second `LiveDevices` pass kept the same PCI per serial. Live handoff saw 11 chart-bearing DPUServices and a nil create error. `cmd/opi-daemon` and `cmd/vsp` `go test` cannot run on dh2 because `go` is not installed. Conformance skips there without envtest binaries. The root permission-denied unit test skips when euid is 0.

Kind: `make test-e2e` failed in `setup-test-e2e` because the Docker socket was absent. Not a VSP logic failure.

## 6. Prerequisites

All required DPF v26.4 pieces that this adapter is allowed to use are already on dh2. Installed cert-manager, Argo CD, and NFD are newer than the doc pins. They were not downgraded. maintenance-operator is `v0.3.0`. DPF system image is `nvcr.io/nvidia/doca/dpf-system:v26.4.0`. `DpuOperatorConfig/dpu-operator-config` reports `VendorOperatorSupported=True` and `VendorOperatorReady=True`. `DPUCluster/dpu-cluster-1` is static, Ready, Kubernetes v1.36.4, one node. Kamaji CRD is absent, which is correct for a static cluster.

Live DPUServices with charts include `default/hbn` v26.4.0 and the `dpu-networking` charts (cni-installer, flannel, multus, IPAM, ovs-cni, service chain, sriov device plugin).

## 7. Boot status at 2026-10-25 09:06Z

| Field | Value |
|---|---|
| Phase | Rebooting |
| Previous phase | DPU Config |
| Age | about 3h42m when read |
| Ready | blank |
| Operational | Unknown |
| OS installed | True at 05:37:47Z |
| BFB prepared and ready | True at 05:21Z |
| Firmware configured | True at 05:21Z |
| Agent last start | 2026-10-25T05:37:40Z |
| NVCONFIG applied | True at 05:38:00Z |
| Reboot method | SystemLevelReset |
| Running firmware | 24.49.1014, pending firmware N/A |
| reset_needed | true |
| Agent text | PCI rescan is required. Reboot external host is required. |

Pending next-boot values: `LAG_RESOURCE_ALLOCATION` 0 to 1, `NUM_OF_VFS` 8 to 46, `NUM_PF_MSIX_VALID` 1 to 0, `PER_PF_NUM_SF` 0 to 1, `PF_BAR2_ENABLE` 1 to 0, `PF_NUM_PF_MSIX` 63 to 228, `PF_NUM_PF_MSIX_VALID` 0 to 1. `PF_TOTAL_SF` and `PF_SF_BAR_SIZE` are not pending.

Operational conditions (NodeProblems, critical and non-critical pods, interfaces, chains, OperationalReady) are Unknown / Pending: waiting for the node to join.

## 8. Attempt to finish the reboot without a host reset

The mapping and the ConfigMap `opi-nvidia-dpf-adapter-mappings` were updated. The manager deployment was restarted and became Ready. Deleting `DPUFlavor/dpf-default-flavor` returned:

```text
admission webhook "vdpuflavor.kb.io" denied the request:
DPUFlavor is being refferred to by DPU(s) [dh2-bluefield-2],
you must delete the DPU(s) first
```

Applying a different spec fails with `DPUFlavor spec is immutable`. The DPU was not deleted. The seven override annotations were removed so the desired spec matches the flavor again. Later logs show the flavor apply succeeding. Phase stayed Rebooting. `/tmp/lab-job-flavor` was removed.

What finishes this phase is a reboot of dh2, which applies the next-boot nvconfig already written on the card. This adapter will not reboot the host and will not run `mlxfwreset`.

## 9. Files touched for this integration

Discovery: `pkg/discovery/pci.go`, `types.go`, `annotator.go`, `inventory.go`, `mock.go`, and the matching tests. VSP: `pkg/vsp/grpc_server.go`, `handoff.go`, `cmd/vsp/main.go`, `cmd/opi-daemon`, `config/vsp/daemonset.yaml`, `config/vsp/rbac.yaml`, `config/overlays/hardware-vsp`. Mapping: `config/mappings/dataprocessingunit.yaml`. Docs: `docs/vsp.md`, `docs/hybrid-integration-design.md`, `docs/lcm-bluefield-reboot.md`, `README.md`. Conformance: `test/conformance/dimensions_test.go`.

## 10. Left open

1. Kind `make test-e2e` until Docker is up and the image contains `/opi-daemon`.

2. `DPU/dh2-bluefield-2` stays Rebooting until the external host reboots.

3. Do not delete that DPU to unlock the flavor. The protection finalizer deprovisions it.

4. Do not commit unless asked. The socket, PCI, inventory, handoff, and flavor-annotation work is still local.
