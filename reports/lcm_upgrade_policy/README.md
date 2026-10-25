# Pinned upgrades and rollback policy

Date: 2026-10-24
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`172.22.1.2`), BlueField-2, k3s `v1.36.4+k3s1`
VPN: F5 Access Connected (this Mac)

In-repo copy: `opi-nvidia-dpf-adapter/docs/lcm-upgrade-policy.md`.

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | Unpinned upgrades, races, timeout-as-success, lost last-known-good |
| [02_fix.md](02_fix.md) | Version policy + serialized Helm upgrade/rollback |
| [03_commands.md](03_commands.md) | VPN, SSH, unit tests, read-only lab inventory |
| [04_results.md](04_results.md) | Test outcomes, lab inventory, security gates |

## Short answer

Owned upgrades are allowed only onto the pin `v26.4.0`. Same version
is a no-op. Downgrade is rejected unless `AllowDowngrade`. Timeouts
and rollbacks are verified against the real release before any
success is recorded. The live lab DPF release stays unmanaged —
this code does not helm-upgrade it.
