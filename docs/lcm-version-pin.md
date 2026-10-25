# DPF version pin: keep v26.4.0

Reviewed 2026-10-24 against official docs (no pin change):

- https://networking-docs.nvidia.com/dpf/26.4.1/doca-platform-framework-v26-4-1
- https://networking-docs.nvidia.com/dpf/26.4.1/platform-support
- https://networking-docs.nvidia.com/dpf/26.4.0/dpf-host-trusted
- https://networking-docs.nvidia.com/dpf/26.4.1/dpf-operator-upgrade-guide

v26.4.1 is an August 2026 patch on 26.4.0. Install is still Helm only.
Upgrade 26.4.0 → 26.4.1 is an allowed patch step. Tested adapters are
**BlueField-3** (B3240 / B3220 / B3210). Helm prerequisite is v3.5+.

This lab is **BlueField-2**. Live DPF is `v26.4.0` and the DPU is stuck
`Rebooting` on pending NVCONFIG. Changing the pin to v26.4.1 would make
LCM want an upgrade of `dpf-operator` on that host. The ClusterHelmClient
refuses the live release name, so that upgrade cannot happen here, and
a firmware reset was not run.

`config/versions.yaml` stays `v26.4.0`. The support window
`[v26.4.0, v27.0.0)` already treats an installed 26.4.1 as Supported
if a later planned upgrade lands it.
