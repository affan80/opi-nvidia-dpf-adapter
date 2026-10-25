# Results (2026-10-23)

## Unit tests — PASS

```
=== RUN   TestHelmFreshInstallConvergesOnPinnedRelease
--- PASS
=== RUN   TestHelmAlreadyInstalledConvergesOnSameRelease
--- PASS
=== RUN   TestHelmFreshAndAdoptedShareOneReleaseIdentity
--- PASS
=== RUN   TestHelmAdoptsOwnedSkewedReleaseOntoPin
--- PASS
=== RUN   TestHelmDoesNotAdoptUnmanagedRelease
--- PASS
=== RUN   TestHelmUpgradeRefusesUnmanaged
--- PASS
=== RUN   TestSafeHelmValuesPinnedAndArgFree
--- PASS
=== RUN   TestHelmInstallerAMDSkipsChart
--- PASS
=== RUN   TestEnsureInstalledHelmFreshAndAdopted
--- PASS
=== RUN   TestHelmClientSourceHasNoCLI
--- PASS
ok  github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/lifecycle  0.436s

GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
ok  github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/lifecycle  0.998s
```

Done-when met:

| Case | Action | Identity | Release count |
|---|---|---|---|
| Fresh install | `installed` | `dpf-operator-system/dpf-operator@v26.4.0` | 1 |
| Already installed (owned) | `adopted` | same | 1 |
| Owned but skewed version | `adopted` (upgrade onto pin) | same | 1 |
| Live-lab style (`owner=helm`) | `unmanaged` | leftover as-is | 1 (untouched) |
| AMD | `skipped` | no chart | 0 |

`TestEnsureInstalledHelmFreshAndAdopted` calls `EnsureInstalled` twice
on one `MemoryHelmClient`: first `installed`, second `adopted`, same
`Settings` (release/namespace/version).

## Lab prerequisites — working (read-only)

| Prerequisite | Installed / configured | Working |
|---|---|---|
| F5 VPN | Connected | ping <your-lab-hosts> 0% loss |
| SSH to dh2 | `<lab-ssh-key>` | `SSH_OK` hostname `dh2` |
| k3s | `v1.36.4+k3s1` | Server Version `v1.36.4+k3s1` |
| cert-manager | ns `cert-manager`, 3 pods Running | CRD `certificates.cert-manager.io` |
| Argo CD | pods in `dpf-operator-system` | CRD `applications.argoproj.io` |
| NFD | gc/master/worker Running | CRDs `nodefeatures.nfd.k8s-sigs.io` |
| Maintenance Operator | pod Running | CRD `nodemaintenances.maintenance.nvidia.com` |
| DPF operator | image `dpf-system:v26.4.0` | controller Running (113 historical restarts) |
| DPF Helm release | secret `sh.helm.release.v1.dpf-operator.v1` | `owner=helm` **unmanaged** |
| Kamaji | CRD absent | OK: static `DPUCluster` does not need it |
| helm CLI on dh2 | **absent** | expected; do not install |
| Adapter | `opi-nvidia-dpf-adapter-controller-manager` Running | Helm **not** wired |
| LCM | `VendorOperatorSupported=True/Supported` | nvidia v26.4.0 in `[v26.4.0, v27.0.0)` |

## Lab Helm adopt decision

```
sh.helm.release.v1.dpf-operator.v1
  owner=helm
  status=deployed
```

HelmInstaller would return `unmanaged`. That is correct. The running
adapter still uses DetectingInstaller (`action=detected`).

## Pipeline state after prereq check (not this task)

| Object | Result |
|---|---|
| BFB `bf-bundle-340` | Ready |
| DPUDevice `mt2321xz0kvx` | Ready / Discovered / NodeAttached |
| DPU `dh2-bluefield-2` | phase **Rebooting**, `OSInstalled=True` |
| DPUCluster `dpu-cluster-1` | Ready / static / `v1.36.4+k3s1` |
| SFC `sfc-chart-probe` `DPFReady` | False / AllServicesReady (HBN app Unknown while DPU reboots) |
| `dpu-cluster-1-cni-installer` | CrashLoop `exec format error` (DPU image on host k3s) |
| `kube-flannel-ds` | CrashLoop: flannel `10.244.0.0/14` vs k3s PodCIDR `10.42.0.0/24` |
| `dpu-cluster-1-sfc-controller` | CrashLoop: no `/var/run/openvswitch/br-sfc.mgmt` on host |

Those CrashLoops are the static-cluster tradeoff (DPF apps target a DPU
control plane; this lab uses host k3s). Not a Helm-client bug. Do not
helm-uninstall DPF to “fix” them.

## How to enable Helm later (not done)

Only after a release is created **by this adapter** (owner label set):

1. Implement a live `HelmClient` that talks to the cluster Helm storage.
2. Set `LifecycleManager.Helm` in `cmd/main.go`.
3. Keep the unmanaged gate so `owner=helm` is never upgraded.
