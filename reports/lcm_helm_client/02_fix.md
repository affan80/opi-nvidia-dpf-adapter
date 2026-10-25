# Fix

## What changed

| File | Change |
|---|---|
| `pkg/lifecycle/helm_client.go` | **new** Helm SDK: `HelmClient`, `MemoryHelmClient`, `HelmInstaller`, `SafeHelmValues`, `OwnedRelease` |
| `pkg/lifecycle/helm_client_test.go` | **new** fresh / already-installed / unmanaged / AMD / no-CLI tests |
| `pkg/lifecycle/installer.go` | actions `installed`, `adopted`, `unmanaged`; DetectingInstaller still detect-only |
| `pkg/lifecycle/ensure.go` | if `Helm` is set and `Installer` is nil, use `HelmInstaller` |
| `pkg/lifecycle/lifecycle_controller.go` | field `Helm HelmClient` |
| `cmd/main.go` | Helm left **nil** (do not adopt live DPF) |
| `go.mod` | comment only: do **not** add `helm.sh/helm/v3` |
| `docs/lcm-helm-client.md` | in-repo record |
| `README.md` | Helm adopt gate + test row |

## How the Helm SDK works

`HelmClient` is the only install surface: `GetRelease`, `Install`,
`Upgrade`. No `os/exec`, no `helm` binary.

`MemoryHelmClient` is the in-process SDK used by tests. One map key
`namespace/name` so a second install cannot create a second release.

`HelmInstaller.Ensure`:

1. AMD → `skipped` (no chart).
2. Build `DefaultInstallConfig` (chart `dpf-operator`, repo NGC doca,
   version `v26.4.0`, release `dpf-operator`, ns `dpf-operator-system`,
   owner `opi-nvidia-dpf-adapter`).
3. Values = `SafeHelmValues` only:
   `kamajiClusterManager.disable=true`, `monitoring.disable=true`.
   Any `HelmArgs` fail `ValidateInstallConfig`.
4. Missing release → `Install` → action `installed`.
5. Release exists and `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter`
   → `Upgrade` only if chart/version drifted → action `adopted`.
   Same name, same namespace, same pin.
6. Release exists with any other owner → action `unmanaged`.
   No upgrade, no owner stamp, no second release.

## Why `go.mod` does not require helm v3

`helm.sh/helm/v3` pulls a Kubernetes client older than **v0.36.0** and
fights `controller-runtime` **v0.24**. The dedicated SDK is this
package's `HelmClient`. The chart pin stays in `config/versions.yaml`.

## What we did **not** do

- Did not helm-install or helm-uninstall DPF on dh2.
- Did not wire `LifecycleManager.Helm` in `cmd/main.go`.
- Did not install the `helm` CLI on the lab server (rule: no package installs).
- Did not apply AMD stub CRDs.
- Did not invent Argo Applications.
