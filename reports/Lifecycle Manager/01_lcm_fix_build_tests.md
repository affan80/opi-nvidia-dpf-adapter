# 01 — Lifecycle Manager: fix, build, and tests

**Date:** 2026-10-25  
**Repo:** `opi-nvidia-dpf-adapter`  
**Go:** language `1.26.0`, toolchain `go1.26.10`  
**K8s modules:** `v0.36.0` (lab k3s `v1.36.4+k3s1`)  
**DPF pin:** Helm chart `dpf-operator` `v26.4.0` from `https://helm.ngc.nvidia.com/nvidia/doca`

This PDF is the build-and-test record. Security is PDF 02. Issue-by-issue
fixes are PDF 03. E2E is PDF 04.

---

## 1. What we built

The Lifecycle Manager is a controller-runtime reconciler on
`DpuOperatorConfig`. `Reconcile` is not a log line. It calls
`EnsureInstalled` and writes LCM-owned conditions only.

### Installer chain (NVIDIA)

1. **Helm** — `ClusterHelmClient` embeds `SecretHelmClient`. Reads Helm v3
   Secrets (`helm.sh/release.v1`). May **claim** a matching pin when
   `lifecycle.opi.nvidia.com/adopt-existing=true`. Install/upgrade/uninstall
   of live name `dpf-operator` returns `ErrHelmMutationNotConfigured`.
   Mutation is allowed only for dedicated probe release `opi-lcm-probe`.
2. **OLM** — `ClusterOLMClient` observes Subscription/CSV. DPF is not an
   OperatorHub package (`dpf.olm.published: false`). Subscribe is
   mutation-not-configured.
3. **Detect** — `DetectingInstaller` looks at CRDs. Never helm-installs.

Fallthrough errors: `ErrHelmMutationNotConfigured`, `ErrOLMNotPresent`,
`ErrOLMMutationNotConfigured`, `ErrOLMPackageNotPublished`,
`ErrNoVendorChart`.

### Scale / concurrency (pointers 1–4)

| Pointer | Problem | Fix |
|---|---|---|
| 1 | 2000 parallel `Reconcile` on one config → 409 | Per-object lock, `RetryOnConflict` on finalizer and status, `MaxConcurrentReconciles=1`, `--leader-elect` default **true** |
| 2 | ClusterHelm Install TOCTOU / ConfigMap 409 | Per-release lock; Create/Update retry; AlreadyExists → conflict retry |
| 3 | ClaimRelease 409 on `sh.helm.release.v1.dpf-operator.v1` | Separate claim lock (not the Helm install lock — nested mutex deadlocked), retry, skip write if already adopted |
| 4 | Users stall at DPU Rebooting | `ObserveNVIDIADPUs` + `VendorDPUReady`; requeue 30s; **no** mlxfwreset |

### Admission and APF (shipped, not forced on live deploy)

- Validating webhook handler registered in `cmd/main.go`. Manifests in
  `config/webhook/`. Not enabled in `config/default` (needs webhook certs).
- APF `PriorityLevelConfiguration` + `FlowSchema` in `config/apf/`.
  Apply with kubectl. No apiserver flags, no `/etc`.

---

## 2. How to connect the lab

### This Mac is the VPN Mac

F5 Access must show Connected. Then:

```bash
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile=$HOME/.ssh/opi_lab_key -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 \
  root@172.22.1.2 'hostname && k3s kubectl get nodes && k3s kubectl get dpuoperatorconfig'
```

### Other Mac (hop)

```bash
ssh kartikey0104@Kartikeys-MacBook-Pro.local \
  'ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
     -o IdentityFile=$HOME/.ssh/opi_lab_key -o UseKeychain=yes \
     -o KexAlgorithms=curve25519-sha256 \
     root@172.22.1.2 "hostname && nproc && free -h"'
```

Do not install F5 on the other Mac. Do not copy `opi_lab_key`.

Job files only:

```bash
JOB=/tmp/lab-job-lcm-retest
ssh ... "mkdir -p $JOB"
# copy compiled tests, then delete $JOB
```

---

## 3. How to build

From `opi-nvidia-dpf-adapter/`:

```bash
export GOTOOLCHAIN=go1.26.10

# Format / vet
gofmt -w pkg/lifecycle cmd
go vet ./pkg/lifecycle/ ./cmd/...

# Binary
go build -o /tmp/manager ./cmd

# Linux image bits (lab is amd64)
GOOS=linux GOARCH=amd64 go test -c -o /tmp/lifecycle.test ./pkg/lifecycle/
```

Docker / Kind gate:

```bash
make docker-preflight
# fails closed if docker.sock is missing (that was the old e2e skip)
```

Image roll used on dh2 (do not invent a sixth roll unless asked):

- `controller:lcm-20261024` — first LCM roll
- `controller:lcm-20261024b` — Secret informer disabled (`DisableFor` Secret)

```bash
# pattern used (linux/amd64), then:
# k3s ctr images import <tar>
```

`make manifests` regenerates `config/rbac/role.yaml` from Kubebuilder
markers. `make verify-manifests` diffs that.

---

## 4. Commands to run — unit and race

```bash
cd opi-nvidia-dpf-adapter
export GOTOOLCHAIN=go1.26.10

go test ./pkg/lifecycle/ -count=1 -timeout 4m
go test -race ./pkg/lifecycle/ -count=1 -timeout 4m
go test ./pkg/mapping/ -count=1 -timeout 3m
go test ./pkg/lifecycle/ -count=1 -run 'TestPatchedGovulnDependencies'
```

Named LCM suites that must stay green:

```bash
go test ./pkg/lifecycle/ -count=1 -timeout 4m -run '
TestLifecycleReconcile|TestLifecycleReportsVersionSkew|TestLifecycleSkew|
TestConcurrentReconcileSameConfigSucceeds|TestMainDefaultsLeaderElect|
TestSetupWithManagerPinsOneWorker|
TestConcurrentClusterHelm|TestClusterHelmClient|
TestClaimRelease|TestConcurrentClaim|TestConcurrentEnsureInstalledAdopt|
TestEnsureInstalledSecretHelm|
TestObserveNVIDIA|TestDPUObserve|TestReconcileWritesVendorDPU|
TestValidateDpuOperator|TestDpuOperatorConfigValidator|TestAPF|TestWebhook
'
```

Stress (build tag, not in default CI):

```bash
go test ./pkg/lifecycle/ -tags=stress -count=1 -timeout 8m \
  -run TestStressThousandsUsers -v
```

Last stress result (2000 users, 2026-10-25):

| Scenario | Result |
|---|---|
| ParseIntent x2000 | OK |
| EnsureInstalled same pin x2000 | OK |
| EnsureInstalled 2000 user configs (one Helm name) | OK |
| Reconcile same DpuOperatorConfig x2000 | OK (was 2000/2000 409) |
| Reconcile 2000 distinct configs in parallel | BREAK — fake-client NotFound under parallel Get (not a live worker-1 path) |
| Helm Ensure/Upgrade same release x2000 | OK, MaxInflight=1 |
| ClusterHelm Install same probe x2000 | OK (was ~1979 409) |
| ClaimRelease same secret x2000 | OK (was ~1996 409) |
| Serial worker=1 EnsureInstalled x2000 | OK, ~11 ms |

---

## 5. Commands to run — verify, lint, vulns

```bash
make verify-rbac
make verify-manifests
make lint
make govulncheck
go mod verify
```

Recorded:

| Command | Result |
|---|---|
| `go test ./pkg/lifecycle/` | ok ~0.7–1.4s (later runs after pointer 1–4) |
| `go test -race ./pkg/lifecycle/` | ok |
| `make govulncheck` | **exit 0**, 0 findings (was exit 3, 43 hits) |
| `make verify-manifests` | PASS when markers match role.yaml |
| `go mod verify` | all modules verified |

---

## 6. Commands to run — Kind e2e

```bash
make docker-preflight
make test-e2e
```

Expected: Kind cluster `opi-nvidia-dpf-adapter-test-e2e` created, **6/6**
PASS, cluster deleted.

| Spec | What |
|---|---|
| Manager runs | controller pod Ready |
| Metrics | serving |
| Mock VSP | stamps DataProcessingUnit serial |
| Emit DPU objects | DPUDevice, DPUFlavor, BFB, DPU |
| Helm-chart NF | DPUService for chart NF; skip image-only |
| HelmProbe | install / upgrade 0.2.0 / rollback 0.1.0 / uninstall **opi-lcm-probe** only |

Never creates live name `dpf-operator`.

Historical fail (saved in-repo `evidence/e2e-tests.log`): docker.sock
missing → `make docker-preflight` now fails closed before Kind.

Full e2e narrative: PDF 04.

---

## 7. Commands to run — compiled tests on dh2

dh2 has **no `go` on PATH**. Compile on the Mac:

```bash
export GOTOOLCHAIN=go1.26.10
GOOS=linux GOARCH=amd64 go test -c -o /tmp/lifecycle.test ./pkg/lifecycle/
# also copy *_test.go helpers that read role.yaml / go.mod:
# config/rbac/role.yaml, go.mod, testdata/
```

Copy into `/tmp/lab-job-lcm-retest/` only. Run the binary there. Delete
the job dir after. First compiled rerun failed when `go.mod` was missing
(`TestPatchedGovulnDependencies`). Retry with go.mod: PASS.

---

## 8. Live checks (read-only / claim only)

```bash
k3s kubectl -n dpf-operator-system get secret \
  -l name=dpf-operator,owner=helm \
  -o jsonpath='{range .items[*]}{.metadata.name} owner={.metadata.labels.owner} managed-by={.metadata.labels.app\.kubernetes\.io/managed-by}{"\n"}{end}'

k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status}/{.reason}{"\n"}{end}'

k3s kubectl -n dpf-operator-system get dpu dh2-bluefield-2 \
  -o jsonpath='phase={.status.phase} method={.status.rebootStatus.method}{"\n"}'
```

Last live snapshot (2026-10-25 ~03:30Z, after VPN reconnect):

- Pod `65b7466b6d-x5qpr`, image `controller:lcm-20261024b`
- Helm secret claimed: adapter owner + `mode=adopted`, storage `owner=helm`
- LCM `action=noop` installed=`v26.4.0` Supported Ready
- `spec.vendor=nvidia`, `adopt-existing=true`
- DPU `dh2-bluefield-2` **Rebooting**, `host-power-cycle-required=true`
- `DPFReady=False`; HBN / flannel / CNI / SFC Pending

---

## 9. Versions after the CVE bump

| Pin | Value |
|---|---|
| go | 1.26.0 |
| toolchain | go1.26.10 |
| k8s.io/* | v0.36.0 |
| controller-runtime | v0.24.1 |
| google.golang.org/grpc | v1.83.2 |
| golang.org/x/net | v0.60.0 |
| golang.org/x/text | v0.42.0 |
| go.opentelemetry.io/otel | v1.45.0 |
| github.com/google/cel-go | v0.30.0 |
| helm.sh/helm/v3 | **not imported** |
| DPF Helm | v26.4.0 |
| DPF window | [v26.4.0, v27.0.0) |
| Kind (CI) | v0.32.0 (not latest) |

---

## 10. What is still not a complete production installer

- NGC `helm install/upgrade` of live `dpf-operator` is refused on purpose.
- Dedicated `opi-lcm-probe` is the disposable mutation path (Kind).
- AMD has no published DSC operator chart. Detect only. No stubs on BF2.
- DPU Rebooting is a host-agent / NVCONFIG gate. LCM reports it; it does
  not reset firmware.
