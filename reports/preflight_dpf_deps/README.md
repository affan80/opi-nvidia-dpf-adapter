# Preflight DPF dependencies

Date: 2026-10-23
Lab: dh2 (172.22.1.2), VPN Connected, k3s v1.36.4+k3s1

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | False `Supported` while Argo CD is missing |
| [02_fix.md](02_fix.md) | `dependency_checker.go`, `Blocked`, installer skipped |
| [03_commands.md](03_commands.md) | Local tests + lab inventory + deploy |
| [04_results.md](04_results.md) | Before/after lab output |
| [05_how_to_unblock.md](05_how_to_unblock.md) | Operator fix for each Blocked case |

In-repo: `opi-nvidia-dpf-adapter/docs/lcm-preflight-deps.md`.
