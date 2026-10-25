# Observe DPF through an OLM path

## Issue

`EnsureInstalled` could detect CRDs and observe Helm storage. It had
no Operator Lifecycle Manager path. Version therefore had to come
from the DPF image window or
`lifecycle.opi.nvidia.com/vendor-operator-version`.

A later step could have:

- installed OLM on this k3s lab (a cluster change this repo must not make)
- invented an OperatorHub package for DPF (NVIDIA does not publish one)
- created a Subscription next to the live Helm `dpf-operator`

Latest DPF docs still install through Helm only:

- https://networking-docs.nvidia.com/dpf/26.4.0/dpf-host-trusted
- https://networking-docs.nvidia.com/dpf/26.4.1/dpf-host-trusted

Done-when: missing `operators.coreos.com` is **not** an error; a
present Subscription/CSV at `dpf-operator-system/dpf-operator`
supplies the version; Helm storage, when present, still wins; no
Subscription is created.

## Fix

`pkg/lifecycle/olm_client.go` is the dedicated OLM observer.

| Piece | Role |
|---|---|
| `OLMClient` | `APIsPresent` / `GetSubscription` / `Subscribe` / `ClaimSubscription`. No CLI. No operator-framework SDK. |
| `MemoryOLMClient` | In-process SDK used by tests. |
| `ClusterOLMClient` | Reads unstructured Subscription + CSV. `Subscribe` is `ErrOLMMutationNotConfigured`. |
| `OLMInstaller` | NVIDIA: observe `dpf-operator-system/dpf-operator`; claim only with `adopt-existing=true` on the exact pin. AMD: skip. |
| `LifecycleManager.OLM` | Wired in `cmd/main.go`. Helm storage wins. Missing APIs fall back to DetectingInstaller. |

`config/versions.yaml` pins `dpf.olm.published: false` with empty
package / channel / catalog. Chart URLs, Helm args, and OLM catalog
overrides on `DpuOperatorConfig` stay `InvalidConfig`.

### Adopt gate

| Existing Subscription | Action |
|---|---|
| OLM APIs absent | fall back to DetectingInstaller (this lab, Kind) |
| APIs present, no Subscription | do not create; fall back to detect |
| owner = `opi-nvidia-dpf-adapter` | already owned — `noop`, version from CSV |
| unmanaged **and** identity is the pin **and** `adopt-existing=true` | `adopted` — stamp adapter owner + `mode=adopted`. No subscribe, no delete. |
| any other owner, or no annotation, or a version that is not the pin | `unmanaged` — do not stamp, do not create a second Subscription |

Identity both OLM paths share when a CSV exists:

```
dpf-operator-system/dpf-operator@v26.4.0
```

## Commands and versions

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestNVIDIAOLM|TestVersionsYAMLPinsOLM|TestRejectOLM|TestClusterOLM|TestOLMInstaller|TestEnsureInstalledOLM|TestEnsureInstalledHelmWins|TestMainWiresOLM|TestOLMClientSource|TestManagerRoleObservesOLM'
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1
GOTOOLCHAIN=go1.26.10 go test -race ./pkg/lifecycle/ -count=1
```

| Tool / pin | Version |
|---|---|
| Go | 1.26.0 / toolchain go1.26.10 |
| Kubernetes modules | v0.36.0 |
| k3s (lab) | v1.36.4+k3s1 |
| DPF | Helm `dpf-operator` v26.4.0 |
| OLM package | unpublished (`published: false`) |

Lab (inventory only — do **not** install OLM or create a Subscription):

```bash
scutil --nc start "F5 Access"
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/opi_lab_key" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@172.22.1.2 '
k3s kubectl get crd subscriptions.operators.coreos.com \
  clusterserviceversions.operators.coreos.com
k3s kubectl get subscription,csv -A
k3s kubectl get secret -n dpf-operator-system -l owner=helm \
  -o jsonpath="{range .items[*]}{.metadata.name}{\"\\n\"}{end}"
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

Want: OLM CRDs **absent**, zero Subscriptions, Helm secret still
present, LCM conditions unchanged. Do not helm-uninstall. Do not
apply AMD stubs.

## Lab result (2026-10-24)

VPN Connected, SSH to dh2 OK. k3s `v1.36.4+k3s1`.

```
subscriptions.operators.coreos.com          NotFound
clusterserviceversions.operators.coreos.com NotFound
subscription/csv objects                    none
Helm dpf-operator secret                    owner=helm
VendorOperatorSupported                     True/Supported
VendorOperatorReady                         True/Ready
```

Compiled `GOOS=linux GOARCH=amd64` `lifecycle.test` (dh2 has no `go`
on PATH; `k3s --version` prints `go1.26.7`). OLM-focused tests
**PASS**. After the run: still no OLM CRDs, still no Subscription,
job dir deleted.

Kind e2e (no OLM APIs, vendored CRDs): **5/5 PASS**, cluster deleted.

Unit tests (Go 1.26.10):

```
TestNVIDIAOLMPackageIsUnpublished                 PASS
TestVersionsYAMLPinsOLMUnpublished                PASS
TestRejectOLMOverrides                            PASS
TestClusterOLMClientAbsentAPIs                    PASS
TestClusterOLMClientDoesNotCreateSubscription     PASS
TestOLMInstallerAbsentAPIsAreNotError             PASS
TestOLMInstallerDoesNotSubscribeWhenAPIsPresent   PASS
TestOLMInstallerObservesCSVVersion                PASS
TestOLMInstallerDoesNotAdoptWithoutConsent        PASS
TestOLMInstallerAdoptsPinnedSubscription          PASS
TestOLMInstallerSkipsAMD                          PASS
TestEnsureInstalledOLMCSVVersionWithoutAnnotation PASS
TestEnsureInstalledHelmWinsOverOLM                PASS
TestEnsureInstalledOLMAbsentFallsBackToDetect     PASS
TestMainWiresOLMClient                            PASS
go test ./pkg/lifecycle/ -count=1                 ok
go test -race ./pkg/lifecycle/ -count=1           ok
```
