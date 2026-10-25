# NVIDIA VSP integration — issues and fixes

Date: 2026-10-25. Work is uncommitted on branch `LifecycleManager`. Nothing below was committed in this stretch.

## Summary

| Issue | Result |
|---|---|
| Daemon and VSP did not share a socket | Fixed in manifests. Live handshake proved on dh2 |
| SoC management `0xc2d3` could look like a second DPU | Fixed. Function 0 BlueField products only |
| Missing PCI or serial could still stamp a DPU | Fixed. `ValidateIdentity` blocks the stamp |
| Stale annotations survived reboot, removal, and a second card | Fixed. Inventory refresh clears or rebinds from a live scan |
| CreateNetworkFunction succeeded before DPF accepted anything | Fixed. Success requires a live chart-bearing DPUService |
| Kind `make test-e2e` never ran the new image | Open. Docker socket down and disk was full |
| dh2 has no `go` binary | Left as-is. Policy forbids installing a toolchain |
| `DPU/dh2-bluefield-2` stuck in Rebooting | Open. Flavor cannot be recreated while the DPU refers to it |
| Override annotations made the manager error-loop | Fixed on the cluster by removing the seven keys. Flavor apply succeeded again |

## 1. Socket was not the daemon socket

Kind used an emptyDir the official daemon cannot see. Hardware needs the host path the dpu-operator already mounts.

Fix: `config/vsp/daemonset.yaml` shares an emptyDir with an `opi-daemon` sidecar. Overlay `config/overlays/hardware-vsp` uses hostPath `/var/run/dpu-daemon` (Bidirectional) and drops the sidecar. `cmd/opi-daemon` is the GrpcPlugin Init/GetDevices client. Directory mode is `0700`. Default socket is `/var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock`.

Live proof: VSP pod `opi-nvidia-dpf-adapter-nvidia-vsp-bfhws` Running. `opi-daemon --once` logged one Healthy device.

## 2. PCI identity was too loose

An NVIDIA fallback treated the SoC management function `0xc2d3` as a DPU. On dh2 that is `0000:0f:00.2`, next to the real Ethernet functions `.0` and `.1` (`0x15b3:0xa2d6`, MT42822). sysfs `serial` is missing. VPD and config space are readable as root.

Fix in `pkg/discovery/pci.go`: products are BlueField-2 `0xa2d2` / `0xa2d6` and BlueField-3 `0xa2dc` / `0xa2dd`. No fallback. `uniqueBySerial` keeps one function 0 device per serial. Serial read order is sysfs `serial`, then VPD `SN`, then the PCIe DSN. Permission denied is a hard error.

Live identity: PCI `0000:0f:00.0`, DPF form `0000-0f-00`, serial `MT2321XZ0KVX`. `.1` is the same serial and is dropped. `.2` is skipped.

## 3. Missing identity still looked provisionable

A DPU with an empty PCI address or serial could pass the mapping.

Fix: `ValidateIdentity` requires both. Mapping fields `spec.serialNumber` and `spec.pciAddress` are `required: true`. The annotator does not stamp an invalid device. `TestApplyBlocksDPUWithoutPCIAddress` covers the mapping.

## 4. Reboot, disappearance, and a second card

Annotations were treated as proof of hardware. After a rescan the PCI address can move. A stale serial could steal another DPU's card. An enumerate error used to look like an empty bus.

Fix: `pkg/discovery/inventory.go`. Refresh every 15s. `LiveDevices` drops invalid identities and returns the enumerate error instead of clearing. `AssignDevices` matches serial, then PCI, then unused cards onto unstamped DPUs (name order against lowest PCI). A stale serial is cleared and cannot steal. Two stale DPUs plus one leftover card are not guessed. One leftover card may replace one cleared DPU. `StripIdentity` removes serial and PCI only. Firmware URL and flavor stay.

Tests: idempotent bind, clear when gone, PCI move, PCI swap, multi-device pair, two-stale-one-card, enumerate failure keeps stamps, annotator follows a hardware change and ignores the other DPU.

## 5. Network-function success was a no-op

Create and Delete returned an empty success before any DPUService existed. That reported a user-facing success with no DPF intent.

Fix: `pkg/vsp/handoff.go`. Selected contract is delegate-to-translator/DPF (DPF 26.4 DPUService guide). The VSP does not Helm-install, Helm-uninstall, or mutate DPF. Create succeeds only when a DPUService exists, is not deleting, and `spec.helmChart.source.chart` is set. Delete is CNI detach: it succeeds when DPF is observable, and a live DPUService is not a delete failure. Nil handoff is `RejectHandoff`. In-cluster VSP sets `NewClusterHandoff`. RBAC on `svc.dpu.nvidia.com/dpuservices` is get, list, and watch only.

Tests: refuse before intent, static handoff, fake client empty / accepted / empty chart / deleting, nil client, RBAC file has no create/update/patch/delete, doc string contains `do not Helm-install` on one line. Live dh2: 11 accepted DPUServices, create error nil.

## 6. Kind end-to-end image

`make test-e2e` must use an image that contains `/opi-daemon`. The run failed before the cluster existed:

```text
failed to connect to the docker API at
unix:///Users/kartikey0104/.docker/run/docker.sock
make: *** [setup-test-e2e] Error 1
```

The Mac data volume had also been full, so the image was not rebuilt. This is still open. Unit and lab hardware tests do not depend on Kind.

## 7. Lab test packaging

Mapping tests failed on `._amd-dsc200.yaml` (AppleDouble) and on a tree that was missing `config/condition-writers.yaml`, CRD fixtures, and sibling Go sources. `cmd/opi-daemon` and `cmd/vsp` tests call `go build` and fail on dh2 with `exec: "go": executable file not found`. Conformance BeforeSuite skips without envtest binaries. The permission-denied PCI test skips when euid is 0.

Fix for the YAML failures: `COPYFILE_DISABLE=1`, delete `._*`, and upload a repo-shaped tree under `/tmp/lab-job-<name>/`. Go was not installed on dh2.

## 8. Reboot is stuck after a successful install

`DPU/dh2-bluefield-2` phase is `Rebooting`, previous phase `DPU Config`. OSInstalled, BFBReady, and FWConfigured are True. OperationalReady is Unknown because the node has not joined. Host agent at 2026-10-25T05:38:00Z: `NVConfigApplied=True`, `RebootMethodDiscovery=True/SystemLevelReset`, `reset_needed=true`, description `PCI rescan is required`, command `Reboot external host is required`. Running firmware `24.49.1014`. Scale functions already match (`PF_TOTAL_SF=32`, `PF_SF_BAR_SIZE=8`). Still pending next boot:

| Parameter | Current | Next boot |
|---|---|---|
| LAG_RESOURCE_ALLOCATION | 0 | 1 |
| NUM_OF_VFS | 8 | 46 |
| NUM_PF_MSIX_VALID | 1 | 0 |
| PER_PF_NUM_SF | 0 | 1 |
| PF_BAR2_ENABLE | 1 | 0 |
| PF_NUM_PF_MSIX | 63 | 228 |
| PF_NUM_PF_MSIX_VALID | 0 | 1 |

Code fix for a future flavor: `config/mappings/dataprocessingunit.yaml` can override those parameters from annotations. `0` on the two scale-function keys still falls back to official 20 and 10. Other parameters keep an explicit 0. `TestApplyFlavorNvconfigMatchesRunningFirmware` passed locally with the dh2 current values.

That fix cannot land on the live flavor. DPF `spec` is immutable. Deleting `dpf-default-flavor` was denied by `vdpuflavor.kb.io`: the flavor is referred to by `DPU/dh2-bluefield-2`, and the DPU must be deleted first. The DPU finalizer `provisioning.dpu.nvidia.com/dpu-protection` deprovisions the card, so the DPU was not deleted. `mlxfwreset` was not run. The host was not rebooted.

Setting the seven current-firmware annotations made the controller report `DPUFlavor spec is immutable` on every reconcile. Those seven annotations were removed. `dpu.nvidia.com/pf-total-sf=32` and `pf-sf-bar-size=8` stayed, because they already match the flavor. The next reconciles logged `applied translated object` for `DPUFlavor/dpf-default-flavor`. Phase is still Rebooting.

## 9. Prerequisites that were checked and left alone

Present and not reinstalled: cert-manager controller (newer than the v26.4.0 doc pin v1.19.3), Argo CD (newer than chart 9.4.1), node-feature-discovery, maintenance-operator `v0.3.0`, DPF system `v26.4.0`. Kamaji is absent. `DPUCluster/dpu-cluster-1` is static and Ready, so Kamaji is not required. Unhealthy flannel, cni-installer, sfc, and sriov pods follow from the DPU node not joining. They were not treated as a separate install.
