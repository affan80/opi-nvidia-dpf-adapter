# TASK 02.01 — EnsureInstalled (LCM)

This is the install-flow contract for the Lifecycle Manager. The design
doc ([hybrid-integration-design.md](hybrid-integration-design.md)) is
still the single implementation contract; this file is the TASK 02.01
record: what was broken, how it was fixed, what is needed, and the
commands that prove it.

## What the issue was

`pkg/lifecycle/lifecycle_controller.go` read `DpuOperatorConfig` and
logged:

```
would install and pin the vendor operator (placeholder for Helm/OLM)
```

That is not an install flow. It could not be unit-tested as
`EnsureInstalled`. A later Helm/OLM step would have been bolted onto a
log line.

Two lab rows look like failures. They are not:

| Lab row | Meaning | Why it stays this way |
|---|---|---|
| installer AMD = **absent** (no DSC CRD) | DetectingInstaller looked up `DSCDevice` / `dpu.amd.com` and found no REST mapping | dh2 is a BlueField host. AMD stub CRDs on that cluster are a fake-device API. Stubs stay in envtest only. |
| LCM wrote Ready = **no** | `DpuOperatorConfig.status.conditions` has `VendorOperatorSupported` only | The dpu-operator daemon owns `Ready`. A second writer flaps the printer column. |

## Approach

Split Reconcile into small units. Reconcile calls `EnsureInstalled`.
The default installer **detects**; it does not mutate Helm.

```
ParseIntent → RunPreflight → Installer.Ensure → EvaluatePolicy → AssessReadiness
     │              │                │                 │                │
  spec.vendor    skip empty      REST-map          ClassifySkew    API present
  then annot.    reject unknown   DPU / DSCDevice   [v26.4, v27)    and not TooOld/TooNew
```

| Unit | File | Job |
|---|---|---|
| intent | `pkg/lifecycle/intent.go` | `ParseIntent`: spec.vendor wins, annotation fallback, `SanitizeVendorToken` |
| preflight | `pkg/lifecycle/preflight.go` | empty → skip (no condition); unknown → `UnknownVendor`, installer not called |
| installer | `pkg/lifecycle/installer.go` | `DetectingInstaller` REST-maps `provisioning.dpu.nvidia.com/DPU` or `dpu.amd.com/DSCDevice`. Helm then OLM observe when wired; missing OLM APIs are not an error (see [lcm-olm.md](lcm-olm.md)). |
| policy | `pkg/lifecycle/policy.go` | `EvaluatePolicy` / window in `version.go` |
| readiness | `pkg/lifecycle/readiness.go` | in-memory `Readiness.Ready` — **not** the `Ready` condition |
| ensure | `pkg/lifecycle/ensure.go` | `LifecycleManager.EnsureInstalled` |

`Installer` is injected on `LifecycleManager`. Tests use a recording
installer to prove Reconcile called `Ensure`. Nil installer →
`DetectingInstaller` on the client's REST mapper.

## What is needed

| Need | Where |
|---|---|
| OPI `DpuOperatorConfig` CRD with `spec.vendor` enum `nvidia\|amd` | `test/e2e/crds/opi`, live cluster on dh2 |
| NVIDIA DPF already installed (this phase does not helm-install it) | dh2: `dpf-operator-system`, image `dpf-system:v26.4.0` |
| Enablement | `spec.vendor: nvidia` and/or annotation `lifecycle.opi.nvidia.com/vendor` |
| Optional version pin | `lifecycle.opi.nvidia.com/vendor-operator-version=v26.4.0` |
| Pins | `config/versions.yaml`, `pkg/lifecycle/version.go` window `[v26.4.0, v27.0.0)` |
| Go 1.26 + `make lint` | CI Ubuntu jobs |
| VPN + lab SSH | F5 Access on this Mac; `opi_lab_key`; do not copy the key |
| Job scratch on the server | `/tmp/lab-job-<name>/` only; delete after |

Not needed, and not allowed in this phase:

- `helm upgrade` / `helm uninstall` of DPF
- AMD `DSC*` CRDs on a BlueField host
- LCM writing condition type `Ready`
- package installs or `/etc` edits on dh2

## Commands

Cwd: `opi-nvidia-dpf-adapter/`.

### Local gates

```bash
go test ./pkg/lifecycle/ -count=1
go test ./pkg/lifecycle/ -count=1 -v -run 'TestReconcileCallsEnsureInstalled|TestParseIntent|TestDetectingInstaller|TestEnsureInstalled'
make lint
make verify-api
```

Done-when: `TestReconcileCallsEnsureInstalled` records one
`Installer.Ensure` call (vendor `nvidia`, pin `v26.4.0`, source `spec`).

### Start the lab

```bash
scutil --nc status "F5 Access" | head -n 8
# if Disconnected:
scutil --nc start "F5 Access"
ping -c 2 -W 2000 172.22.1.2

SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=25
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/opi_lab_key"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
)
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'hostname; k3s --version'
```

### Live probes (dh2)

```bash
# Intent + LCM condition (must be VendorOperatorSupported, never Ready)
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl get dpuoperatorconfig dpu-operator-config -o yaml'

# NVIDIA API present, AMD API absent
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl get crd dpus.provisioning.dpu.nvidia.com dscdevices.dpu.amd.com'

# DPF version the policy window is compared to
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl -n dpf-operator-system get deploy dpf-operator-controller-manager \
     -o jsonpath="{.spec.template.spec.containers[0].image}{"\n"}"'

# After deploy: EnsureInstalled log, not "would install"
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl -n opi-nvidia-dpf-adapter-system logs \
     deploy/opi-nvidia-dpf-adapter-controller-manager --tail=80 \
   | grep ensure'
```

### Deploy the new manager (detect only; no Helm)

```bash
# this Mac, linux/amd64 for dh2
docker build --platform linux/amd64 -t controller:latest .
docker save controller:latest | gzip > /tmp/controller-ensure.tar.gz

ssh "${SSH_OPTS[@]}" root@172.22.1.2 'mkdir -p /tmp/lab-job-ensure'
scp "${SSH_OPTS[@]}" /tmp/controller-ensure.tar.gz \
  root@172.22.1.2:/tmp/lab-job-ensure/controller.tar.gz

ssh "${SSH_OPTS[@]}" root@172.22.1.2 'set -e
gzip -dc /tmp/lab-job-ensure/controller.tar.gz | k3s ctr images import -
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout restart \
  deploy/opi-nvidia-dpf-adapter-controller-manager
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout status \
  deploy/opi-nvidia-dpf-adapter-controller-manager --timeout=180s
rm -rf /tmp/lab-job-ensure
'
```

## Lab result 2026-10-23

```
ensure installed vendor=nvidia source=spec action=detected
  installed=v26.4.0 skew=Supported ready=true
```

That result was **before** the dependency preflight. dh2 is missing
Argo CD, so the current gate reports `Blocked` instead of treating a
detected DPF CRD as success. See
[lcm-preflight-deps.md](lcm-preflight-deps.md).

`ready=true` here is `AssessReadiness` (API present + Supported). It is
**not** a `Ready` condition on the object.

| Check | Got | Want |
|---|---|---|
| intent.vendor / source | nvidia / spec | nvidia / spec |
| preflight | ok | ok |
| installer NVIDIA | detected | detected |
| installer AMD | **absent** | **absent** |
| DPF image | `dpf-system:v26.4.0` | `dpf-system:v26.4.0` |
| policy.skew | Supported | Supported |
| `VendorOperatorSupported` | True/Supported | True/Supported |
| LCM wrote `Ready` | **no** | **no** |
| no-vendor config | no condition | no condition |
| annotation `acme` | False/UnknownVendor, installer skipped | same |

Skip and acme objects were deleted after the probe.
