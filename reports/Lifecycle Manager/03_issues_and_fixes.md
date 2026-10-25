# 03 — Lifecycle Manager: issues and how we fixed them

**Date:** 2026-10-25  
**Rule:** say what the issue was, where it lived, what changed, how we
proved it. NVIDIA only unless noted.

---

## Completeness gap (why LCM was “not done”)

LCM detect/observe/claim is live on dh2 for the NVIDIA Helm pin.
Production NGC install/upgrade/uninstall of `dpf-operator` is **still
not complete** on purpose (`ClusterHelmClient` refuses the live name).
That is a product limit, not an unfinished detect path.

The five LCM gaps we closed in code:

---

### Issue 1 — Helm was nil in `cmd/main.go`

**Where:** `cmd/main.go`  
**What broke:** EnsureInstalled never saw Helm storage. Live DPF looked
like “detect only” even after a Secret client existed.  
**Fix:** wire `lifecycle.NewClusterHelmClient(mgr.GetClient())`. Empty
`ChartRoot` → observe/claim only. Live name refused for mutation.  
**Test:** `TestMainWiresClusterHelmClient`,
`TestEnsureInstalledSecretHelmReportsUnmanaged`.

---

### Issue 2 — Live lab DPF was unmanaged

**Where:** Helm Secret `sh.helm.release.v1.dpf-operator.v1`, `owner=helm`  
**What broke:** matching pin stayed unmanaged; operators could not claim
without a silent takeover.  
**Fix:** annotation `lifecycle.opi.nvidia.com/adopt-existing=true|pin`
plus exact pin match. Stamps adapter owner + `mode=adopted`. Does not
upgrade or uninstall. Cleanup default **retain**.  
**Lab:** annotation set; reconcile `action=noop` `installed=v26.4.0`.  
**Test:** `TestEnsureInstalledSecretHelmClaimsMatchingPin`.

---

### Issue 3 — No OLM path

**Where:** installer chain  
**What broke:** no observation of OperatorHub; people assumed DPF was an
OLM package. Latest NVIDIA docs: Helm only.  
**Fix:** `ClusterOLMClient` observe-only. Pin `dpf.olm.published: false`.
Missing APIs / unpublished / mutation-not-configured fall through to
detect.  
**Test:** `pkg/lifecycle/olm_client_test.go` (compiled-lab PASS on dh2).

---

### Issue 4 — AMD has no chart

**Where:** `DefaultInstallConfig(VendorAMD)`, Helm/OLM skip  
**What broke:** skip returned **success**, hiding “no DSC operator”.  
**Fix:** `ErrNoVendorChart` fallthrough to `DetectingInstaller`. Do not
pin AMD Network Operator or Lago. Do **not** apply
`dscdevices.dpu.amd.com` stubs on BlueField (fake-device risk).  
**Why dh2 still NotFound:** BlueField has no Pensando (`1dd8`). NotFound
is required on this host.  
**Test:** `amd_lcm_test.go`.

---

### Issue 5 — Helm mutation vs k8s v0.36

**Where:** cannot import `helm.sh/helm/v3`  
**What broke:** no real install/upgrade/rollback in CI.  
**Fix:** `ClusterHelmClient` local charts
`pkg/lifecycle/testdata/lcm-probe/{0.1.0,0.2.0}`. Dedicated release
`opi-lcm-probe`. Kind spec HelmProbe **PASS**. Live `dpf-operator`
still refused.

---

## Pipeline / lab issues

### Missing Argo / cert-manager looked like success

**Fix:** `CheckDependencies` + `SkewBlocked`. ReadyClusterProbe includes
cert-manager + Argo (lab Argo lives in `dpf-operator-system`).  
**Test:** dependency_checker tests; lab: Argo API present.

### Secret watch forbidden after first image roll

**Where:** `lcm-20261024` pod  
**What:** `secrets is forbidden: cannot watch`  
**Fix:** `DisableFor` Secret; roll `lcm-20261024b`. Watch errors **0**.

### Live ClusterRole older than generated

**What:** missing dpuclusters / dpfoperatorconfigs list, config finalizer
update.  
**Fix:** apply generated role under live name
`opi-nvidia-dpf-adapter-manager-role`. No wildcards.  
**Test:** `TestManagerRoleHasNoWildcards`.

### dh2 compiled tests FAIL without sources

**What:** `go test -c` binary + missing `go.mod` / `role.yaml` /
`*.go` for source-read tests.  
**Fix:** copy those into `/tmp/lab-job-*`. Then PASS.

### Kind e2e skipped (docker.sock)

**Fix:** `make docker-preflight` before `setup-test-e2e`. Later **6/6**.

### Pin 26.4.1 temptation

**Finding:** official 26.4.1 is a BF3-tested Helm patch (Aug 2026). Lab
is BF2, DPU already Rebooting.  
**Decision:** pin stays **v26.4.0**. Documented in `docs/lcm-version-pin.md`.

---

## Scale pointers (2000-user test)

### Pointer 1 — same DpuOperatorConfig 409

**Error:** `object was modified` on finalizer Update and Status().Update  
**Fix:** lock + RetryOnConflict + one worker + leader-elect true.  
**Proof:** 32-way unit PASS; stress 2000 same object OK.

### Pointer 2 — ClusterHelm Install race

**Error:** `configmaps "opi-lcm-probe": object was modified`  
**Fix:** release lock; applyOne/storeSecret/markSuperseded retry.  
**Proof:** 32-way install one winner; 32-way upgrade 0.2.0; stress OK.

### Pointer 3 — ClaimRelease 409

**Error:** `secrets "sh.helm.release.v1.dpf-operator.v1": object was modified`  
**Deadlock:** first ClaimRelease used the same mutex as Helm Ensure →
`TestEnsureInstalledSecretHelmClaimsMatchingPin` timed out **6m**.  
**Fix:** `claimReleaseLocks` + retry + idempotent skip.  
**Proof:** second claim 0 extra Updates; 2000-way OK.

### Pointer 4 — DPU Rebooting (user flow)

**What:** LCM Ready, dataplane not. Phase Rebooting,
`host-power-cycle-required`, `rebootStatus.method=SystemLevelReset`,
pending NVCONFIG PF_SF_BAR_SIZE / PF_TOTAL_SF.  
**Fix (software only):** `ObserveNVIDIADPUs`, `VendorDPUReady=False`,
requeue 30s, message names the DPU. **No** mlxfwreset, **no** host
reboot, **no** DPU spec edit.  
**Not fixed:** the BlueField is still Rebooting until a human does the
host-agent reset.

---

## Condition writer model (do not regress)

| Object | Type | Writer |
|---|---|---|
| DpuOperatorConfig | Ready | vsp-opi-daemon |
| DpuOperatorConfig | VendorOperatorSupported | lifecycle |
| DpuOperatorConfig | VendorOperatorReady | lifecycle |
| DpuOperatorConfig | VendorOperatorCleanup | lifecycle |
| DpuOperatorConfig | VendorDPUReady | lifecycle |
| DataProcessingUnit | Ready | vsp-opi-daemon |
| DataProcessingUnit | DPFReady | mapping:dataprocessingunit |

LCM must never write `Ready`. Tests fail if it does.

---

## What we did not change on the server

No package install, no `/etc`, no `systemctl`, no reboot, no firmware,
no Ansible, no new SSH keys, no BMC, no helm-uninstall, no AMD stubs.

---

## How to re-prove the issue list

```bash
export GOTOOLCHAIN=go1.26.10
go test ./pkg/lifecycle/ -count=1
go test -race ./pkg/lifecycle/ -count=1 \
  -run 'TestConcurrentReconcile|TestConcurrentClusterHelm|TestConcurrentClaim|TestObserveNVIDIA'
make govulncheck
make docker-preflight && make test-e2e
```
