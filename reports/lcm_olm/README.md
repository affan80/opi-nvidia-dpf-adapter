# Observe DPF through an OLM path

Date: 2026-10-24
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`172.22.1.2`), BlueField-2, k3s `v1.36.4+k3s1`
VPN: F5 Access Connected (this Mac is the VPN Mac)

In-repo copy: `opi-nvidia-dpf-adapter/docs/lcm-olm.md`.

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | No OLM path; version only from annotation/image |
| [02_fix.md](02_fix.md) | Detect-only OLM observer; unpublished pin; Helm wins |
| [03_commands.md](03_commands.md) | TDD, unit, race, Kind e2e, lab inventory |
| [04_results.md](04_results.md) | Unit + Kind 5/5 + dh2: OLM CRDs absent, no Subscription created |

## Short answer

Latest NVIDIA DPF docs (v26.4.0 / v26.4.1) install through **Helm**,
not OperatorHub. This lab has no `operators.coreos.com` APIs.

The LCM now has an OLM path that **observes** a Subscription/CSV when
those APIs exist, and **skips** when they do not. It never creates a
Subscription. Helm storage, when present, still wins.
