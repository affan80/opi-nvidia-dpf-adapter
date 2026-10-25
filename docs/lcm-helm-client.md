# Install and adopt DPF through a Helm client

## Issue

`EnsureInstalled` had a pinned `InstallConfig` and a detecting installer.
There was no Helm SDK. A later step could have:

- shelled out to `helm` with user `--set` / chart URL
- created a second release next to the existing `dpf-operator`
- adopted the live lab release even though Helm, not this adapter, owns it

Done-when: fresh-install and already-installed cases converge on **one**
release: `dpf-operator-system/dpf-operator@v26.4.0` owned by
`opi-nvidia-dpf-adapter`.

## Fix

`pkg/lifecycle/helm_client.go` is the dedicated Helm SDK.

| Piece | Role |
|---|---|
| `HelmClient` | `GetRelease` / `Install` / `Upgrade`. No CLI. |
| `MemoryHelmClient` | In-process SDK used by tests. Same map key = one release. |
| `HelmInstaller` | NVIDIA: install if absent; claim a matching pin when `adopt-existing=true`; otherwise adopt only if already owned. AMD: skip. |
| `SafeHelmValues` | Only `kamajiClusterManager.disable=true` and `monitoring.disable=true`. Rejects `HelmArgs`. |
| `LifecycleManager.Helm` | Wired in `cmd/main.go` as `ClusterHelmClient`. Observes Helm Secrets. Mutates only `opi-lcm-probe`. Refuses the live `dpf-operator` name. Missing storage falls through to OLM (if APIs exist) then DetectingInstaller. |

`go.mod` stays on **Go 1.26.0** with `toolchain go1.26.10` and
Kubernetes **v0.36.0**. The Helm Go module is **not** imported: it
pulls an older Kubernetes client and fights `controller-runtime` v0.24.
`SecretHelmClient` reads `helm.sh/release.v1` Secrets instead.
`cmd/main.go` sets `Client.Cache.DisableFor` to `corev1.Secret` so
the manager does not start a cluster-wide Secret informer (RBAC is
get/list/update/patch, not watch). The
chart pin stays in `config/versions.yaml`. Owned upgrades and rollback
are documented in [lcm-upgrade-policy.md](lcm-upgrade-policy.md).
Install/upgrade/rollback stay `ErrHelmMutationNotConfigured` until a
Helm SDK that keeps k8s v0.36 can be added. Uninstall still refuses
unmanaged releases.

### Adopt gate

| Existing release | Action |
|---|---|
| none | `installed` — write the pinned owned release |
| owner = `opi-nvidia-dpf-adapter` | already owned — upgrade onto the pin if drifted; same version is `noop` |
| `owner=helm` **and** identity is the pin **and** `lifecycle.opi.nvidia.com/adopt-existing=true` | `adopted` — stamp adapter owner + `mode=adopted`. No upgrade, no uninstall, no second release. Helm `owner=helm` stays on the Secret. Cleanup defaults to retain. |
| any other owner, or `owner=helm` without the annotation, or a version that is not the pin | `unmanaged` — do not stamp, do not upgrade, do not create a second release |

Identity both paths must share:

```
dpf-operator-system/dpf-operator@v26.4.0
```

## Commands and versions

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestHelmFresh|TestHelmAlready|TestHelmFreshAndAdopted|TestHelmAdopts|TestHelmDoesNotAdopt|TestHelmUpgrade|TestSafeHelm|TestHelmInstallerAMD|TestEnsureInstalledHelm|TestHelmClientSource|TestSupportedUpgrade|TestSameVersion|TestUnsupported|TestDowngrade|TestConcurrent|TestUpgradeFailure|TestTimeout|TestUnknown|TestRetry|TestSuccessfulRollback|TestFailedRollback'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

| Tool / pin | Version |
|---|---|
| Go | 1.26.0 |
| Kubernetes modules | v0.36.0 |
| k3s (lab) | v1.36.4+k3s1 |
| DPF chart | `dpf-operator` v26.4.0 |
| Chart repo | `https://helm.ngc.nvidia.com/nvidia/doca` |
| Release / namespace | `dpf-operator` / `dpf-operator-system` |
| Owner label | `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter` |

Lab (inventory only — do **not** helm-install or helm-uninstall):

```bash
scutil --nc start "F5 Access"
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/opi_lab_key" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@172.22.1.2 '
k3s --version
k3s kubectl version --short
k3s kubectl get crd certificates.cert-manager.io applications.argoproj.io \
  nodefeatures.nfd.k8s-sigs.io maintenances.maintenance.nvidia.com
helm version
helm list -n dpf-operator-system
helm get metadata dpf-operator -n dpf-operator-system
k3s kubectl get secret -n dpf-operator-system -l owner=helm \
  -o jsonpath="{range .items[*]}{.metadata.name} owner={.metadata.labels.owner} managed-by={.metadata.labels.app\\.kubernetes\\.io/managed-by}{\"\\n\"}{end}"
k3s kubectl get dpuoperatorconfig dpu-operator-config -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

If `helm get metadata` shows `v26.4.0` and the Secret still has only
`owner=helm`, HelmInstaller reports `unmanaged` unless
`DpuOperatorConfig` has `lifecycle.opi.nvidia.com/adopt-existing=true`.
That annotation is the only consent to claim the live pin. Do not
helm-install or helm-uninstall. `ClaimRelease` re-Gets the Secret,
retries 409s, and skips a second write when owner+mode are already
stamped. It uses a lock that is not the Helm install lock (nested
acquire deadlocks).

## Lab result (2026-10-23 19:51 UTC)

VPN Connected, SSH to dh2 OK. k3s `v1.36.4+k3s1`. cert-manager, Argo CD,
NFD, and Maintenance Operator are Running. DPF image
`nvcr.io/nvidia/doca/dpf-system:v26.4.0`. Helm secret
`sh.helm.release.v1.dpf-operator.v1` has `owner=helm` — unmanaged.
LCM `VendorOperatorSupported=True/Supported`. DPU
`dh2-bluefield-2` phase `Rebooting`, `OSInstalled=True`.

Unit tests (Go 1.26.0):

```
TestHelmFreshInstallConvergesOnPinnedRelease          PASS
TestHelmAlreadyInstalledConvergesOnSameRelease        PASS
TestHelmFreshAndAdoptedShareOneReleaseIdentity        PASS
TestHelmAdoptsOwnedSkewedReleaseOntoPin               PASS
TestHelmDoesNotAdoptUnmanagedRelease                  PASS
TestEnsureInstalledHelmFreshAndAdopted                PASS
go test ./pkg/lifecycle/ -count=1                     ok 0.998s
```

Identity both owned paths share: `dpf-operator-system/dpf-operator@v26.4.0`.
