# Protect externally owned installations

Date: 2026-10-24
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`172.22.1.2`)
VPN: F5 Access Connected

In-repo copy: `opi-nvidia-dpf-adapter/docs/lcm-cleanup.md`.

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | Cleanup could delete external DPF, shared CRDs, or other tenants' objects |
| [02_fix.md](02_fix.md) | Ownership classes, removal policy, revalidation, scoped deletes |
| [03_commands.md](03_commands.md) | Unit tests and read-only lab inventory |
| [04_results.md](04_results.md) | Test outcomes and lab proof the live release stays |

## Short answer

A release is deleted only when it is controller-owned (or adopted
with an explicit `delete-owned` policy) **and** the removal policy
allows it. External Helm (`owner=helm`), missing or conflicting
labels, shared CRDs, and referenced dataplane are preserved. The
live lab DPF is external. It was not uninstalled.
