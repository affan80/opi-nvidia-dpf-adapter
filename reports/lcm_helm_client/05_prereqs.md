# Prerequisite check (before Helm work)

Checked first, then Helm client was implemented. Lab read-only.

## Connection

```
scutil --nc start "F5 Access"
# first attempt: Connecting, ping 100% loss, SSH timed out
# stop/start: Disconnected
# start again: Connected after ~29 tries (~90s)
ping 172.22.1.2 → 0% loss (309–391 ms)
ssh root@172.22.1.2 → SSH_OK dh2
```

## What is installed and working

| Need | Where | Status |
|---|---|---|
| Kubernetes 1.36.4 | k3s | `Client` and `Server` `v1.36.4+k3s1` |
| cert-manager | `cert-manager` ns | 3/3 Running; CRD present |
| Argo CD | `dpf-operator-system` (not `argocd` ns) | 4/4 Running; `applications.argoproj.io` |
| NFD | `dpf-operator-system` | gc + master + worker Running |
| Maintenance Operator | `dpf-operator-system` | pod Running; CRD is `nodemaintenances.maintenance.nvidia.com` (not `maintenances.*`) |
| DPF | Helm secret + image `v26.4.0` | operator Running |
| Adapter | `opi-nvidia-dpf-adapter-system` | manager + VSP Running |
| LCM condition | `dpu-operator-config` | `VendorOperatorSupported=True/Supported` |

## What is not a Helm-client blocker

| Item | Why it is OK |
|---|---|
| `helm` binary missing on dh2 | Adapter uses `HelmClient`, not CLI. Do not install packages. |
| Kamaji CRD missing | `DPUCluster` type=`static`. Checker only requires Kamaji for `type=kamaji`. |
| Live release `owner=helm` | Adopt must refuse. Tests cover `unmanaged`. |
| DPU phase Rebooting | OS install finished (`OSInstalled=True`). DPF, not LCM. |
| CrashLoop CNI / flannel / SFC | Static cluster scheduled DPU apps onto host k3s. Separate issue. |

## Helm secrets on dh2 (all `owner=helm`)

```
sh.helm.release.v1.argo-cd.v1                  deployed
sh.helm.release.v1.dpf-operator.v1             deployed
sh.helm.release.v1.maintenance-operator.v1     deployed
sh.helm.release.v1.node-feature-discovery.v1   deployed
```

None carry `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter`.
Adopt all four would be wrong. Only `dpf-operator` is in scope, and it
stays unmanaged.
