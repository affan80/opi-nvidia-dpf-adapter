# BlueField reboot diagnostics (no reset)

Gathered read-only on dh2. No `mlxfwreset`, no host reboot, no Helm
change, no DPU spec edit.

| Field | Value |
|---|---|
| DPU | `dpf-operator-system/dh2-bluefield-2` |
| Phase | `Rebooting` since 2026-10-23 |
| Annotation | `provisioning.dpu.nvidia.com/host-power-cycle-required=true` |
| rebootMethod | `SystemLevelReset` |
| BFB | `bf-bundle-340` prepared/ready |
| Device | `mt2321xz0kvx` PCI `0000-0f-00` serial `MT2321XZ0KVX` |

Host agent `NVConfigApplied` is True and `reset_needed` stays true.
Scale functions already match the flavor (`PF_TOTAL_SF=32`,
`PF_SF_BAR_SIZE=8`). The pending next-boot values are the other HBN
parameters (`NUM_OF_VFS` 8→46, `PF_NUM_PF_MSIX` 63→228, `LAG_RESOURCE_ALLOCATION`
0→1, plus `PF_BAR2_ENABLE`, `PER_PF_NUM_SF`, and the MSIX-valid flags).
DPF wants:

`mlxfwreset -d /dev/mst/mt41686_pciconf0 reset --level 3 --type 0 --sync 1 --method 0`

(and the same on `.1`). That is a PCI/driver reset. The lab rule is not
to run it from this adapter. `dpu-cluster-1-cni-installer` is Error
(expected while the DPU is not Ready).

LCM `VendorOperatorReady=True` is correct: operator CRDs and controllers
are healthy. DPU reboot is not an LCM install failure.

`ObserveNVIDIADPUs` lists `provisioning.dpu.nvidia.com/DPU` and writes
`VendorDPUReady=False` / `Rebooting` when phase is Rebooting / ARM force
restart / checking host reboot, when `rebootStatus.method` is
`SystemLevelReset` / `FirmwareReset` / `PowerCycle` (and not Succeeded),
or when `host-power-cycle-required=true`. The condition message includes
the host-agent pending NVCONFIG list (`NUM_OF_VFS` 8→46 and the other
HBN next-boot deltas on dh2). Reconcile requeues every **5s** and
SetupWithManager also **watches** DPU objects so a phase change does not
wait on the timer. It does not patch the DPU, does not run
`mlxfwreset`, and does not power-cycle the host.

Official DPF 26.4.0 DPUFlavor HBN nvconfig uses `PF_TOTAL_SF=20` and
`PF_SF_BAR_SIZE=10` (`https://networking-docs.nvidia.com/dpf/26.4.0/dpuflavor`).
`spec.nvconfig[].hostPowerCycleRequired` is deprecated and unused.
BFB ≥ 2.7 can warm-reboot; the live annotation forces a host power cycle
(`https://networking-docs.nvidia.com/dpf/26.4.1/dpuset`). Reset outside
DPF APIs may not recover (`https://networking-docs.nvidia.com/dpf/26.4.1/doca-platform-framework-v26-4-0`).

`DPU.spec.dpuFlavor` is immutable, so a stuck DPU cannot be pointed at a
new flavor name. A same-named `DPUFlavor` can be recreated only before a
DPU refers to it. On dh2 (2026-10-25) `vdpuflavor.kb.io` denied the
delete: `DPUFlavor is being refferred to by DPU(s) [dh2-bluefield-2],
you must delete the DPU(s) first`. Applying a different `spec` fails
with `DPUFlavor spec is immutable`. Do not delete that DPU from this
adapter: finalizer `provisioning.dpu.nvidia.com/dpu-protection`
deprovisions it.

The annotation overrides still matter for a flavor created first.
SF uses `dpu.nvidia.com/pf-total-sf` / `dpu.nvidia.com/pf-sf-bar-size`
(dh2: 32 / 8); `0` falls back to official 20/10. The other parameters
(`pf-bar2-enable`, `per-pf-num-sf`, `num-pf-msix-valid`,
`pf-num-pf-msix-valid`, `pf-num-pf-msix`, `num-of-vfs`,
`lag-resource-allocation`) keep an explicit `0`. Official defaults apply
when the annotation is absent. Setting them on the live DPU made the
controller retry an immutable apply, so those seven keys were removed
again and `DPUFlavor` apply succeeded. The card's next-boot values are
already written (`NUM_OF_VFS` 8→46, `PF_NUM_PF_MSIX` 63→228, and the
other HBN deltas). The host agent reports `reset_needed: true` and
`command_required: Reboot external host is required`. This adapter still
does not run `mlxfwreset` and does not reboot the host.
