# LCM RBAC, tests, and security report

Date: 2026-10-24
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`<your-lab-hosts>`), k3s `v1.36.4+k3s1`
VPN: F5 Access (SSH to dh2 OK)

In-repo copy: `opi-nvidia-dpf-adapter/docs/lcm-security.md`.

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | Why RBAC, tests, and supply-chain checks were required |
| [02_fix.md](02_fix.md) | What changed in code and CI |
| [03_commands.md](03_commands.md) | VPN, SSH, unit, race, lab inventory |
| [04_results.md](04_results.md) | Pass/fail/blocked, live ClusterRole gap, vulns |

## Short answer

Lifecycle tests **PASS** on dh2 (full `pkg/lifecycle` package). Generated
RBAC has no wildcards and does not delete CRDs or `DpuOperatorConfig`.
The live ClusterRole is an **older** deploy (missing `dpuclusters` and
`dpfoperatorconfigs` list, and config finalizer update). Live DPF is
`owner=helm` and was **not** uninstalled. `govulncheck` findings are
tracked exceptions (Go 1.26 stdlib / `x/net` / grpc / otel).
