# TASK 02.01 — Replace the LCM logging placeholder

Date: 2026-10-23
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`172.22.1.2`), BlueField-2, k3s `v1.36.4+k3s1`, VPN F5 Access Connected

In-repo copy of this record: `opi-nvidia-dpf-adapter/docs/lcm-ensure-installed.md`.

| File | What it is |
|---|---|
| [01_issue.md](01_issue.md) | What was broken, including AMD absent and Ready=no |
| [02_approach.md](02_approach.md) | Units, files, what is needed, what is forbidden |
| [03_commands.md](03_commands.md) | Local gates, VPN/SSH, live probes, image import |
| [04_results.md](04_results.md) | Unit + lab results from 2026-10-23 |

## Short answer

Reconcile now calls `EnsureInstalled` (intent → preflight → installer →
policy → readiness) instead of logging `would install`.

- **installer AMD = absent**: correct. No DSC CRD on a BlueField host.
- **LCM wrote Ready = no**: correct. Daemon owns `Ready`; LCM writes
  `VendorOperatorSupported` only.
