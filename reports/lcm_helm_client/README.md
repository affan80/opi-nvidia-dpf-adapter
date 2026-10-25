# Install and adopt DPF through a Helm client

Date: 2026-10-23 (lab inventory 19:51 UTC)
Repo: `opi-nvidia-dpf-adapter`
Lab: dh2 (`<your-lab-hosts>`), BlueField-2, k3s `v1.36.4+k3s1`
VPN: F5 Access Connected (this Mac is the VPN Mac)

In-repo copy: `opi-nvidia-dpf-adapter/docs/lcm-helm-client.md`.

| File | What |
|---|---|
| [01_issue.md](01_issue.md) | No Helm SDK; risk of CLI, second release, adopt of unmanaged DPF |
| [02_fix.md](02_fix.md) | `helm_client.go` + pins + adopt gate + why `go.mod` has no helm v3 |
| [03_commands.md](03_commands.md) | VPN, SSH, unit tests, lab inventory (read-only) |
| [04_results.md](04_results.md) | Unit + lab results, versions |
| [05_prereqs.md](05_prereqs.md) | Prerequisite check before the Helm work |

## Short answer

Fresh-install and already-installed **owned** cases converge on one
release: `dpf-operator-system/dpf-operator@v26.4.0` owned by
`opi-nvidia-dpf-adapter`.

The live lab `dpf-operator` Helm secret is `owner=helm`. That is
**unmanaged**. HelmInstaller must **not** adopt it. `cmd/main.go`
leaves `LifecycleManager.Helm` nil so the running adapter stays
detect-only.
