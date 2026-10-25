# 04 — Lifecycle Manager: end-to-end report

**Date:** 2026-10-25  
**Two e2e surfaces:** disposable Kind (this Mac) and live dh2 (BlueField-2).

---

## 1. Kind e2e (disposable cluster)

### Purpose

Prove the adapter manager, metrics, mock VSP, NVIDIA emit path, chart NF,
and **Helm mutation on a dedicated release** — never live `dpf-operator`.

### Commands

```bash
cd opi-nvidia-dpf-adapter
export GOTOOLCHAIN=go1.26.10
make docker-preflight
make test-e2e
```

`docker-preflight` must see a working Docker engine. If
`unix:///Users/kartikey0104/.docker/run/docker.sock` is missing, the
target **fails closed**. That is intentional.

### Historical failure (in-repo evidence)

File: `opi-nvidia-dpf-adapter/evidence/e2e-tests.log`

```
failed to connect to the docker API at unix:///Users/kartikey0104/.docker/run/docker.sock
make: *** [setup-test-e2e] Error 1
```

Sandbox runs also got `permission denied` on the socket. That log is
**not** the current result. It is the reason preflight exists.

### Current result (after preflight)

Kind cluster name: `opi-nvidia-dpf-adapter-test-e2e`  
Outcome: **6/6 PASS**  
Cluster: **deleted** at the end of the suite (each run).

| # | Spec | What it proves | Result |
|---|---|---|---|
| 1 | Manager runs | controller Deployment Ready | PASS |
| 2 | Metrics | metrics endpoint serving | PASS |
| 3 | Mock VSP | unix VSP stamps DataProcessingUnit serial | PASS |
| 4 | Emit DPF objects | DPUDevice, DPUFlavor, BFB, DPU from annotated DPU | PASS |
| 5 | Helm-chart NF | DPUService for chart NF; image-only NF skipped | PASS |
| 6 | HelmProbe | install 0.1.0 → upgrade 0.2.0 → rollback 0.1.0 → uninstall | PASS |

### HelmProbe (the LCM e2e)

File: `test/e2e/helm_probe_test.go`  
Identity: namespace/name **`opi-lcm-probe`**, chart
`pkg/lifecycle/testdata/lcm-probe/{0.1.0,0.2.0}`.

Guarantees:

- Never uses release name `dpf-operator`
- Never talks to NGC
- Uninstall leaves no probe Secret/ConfigMap
- Proves `ClusterHelmClient` mutation without importing Helm SDK

Unit companion (faster than Kind):

```bash
go test ./pkg/lifecycle/ -count=1 -run 'TestClusterHelmClient|TestConcurrentClusterHelm'
```

### Kind versions

CI pins Kind **v0.32.0** (not `latest`). Kubernetes fixtures align with
the v1.36 pin.

---

## 2. Live lab user-flow e2e (dh2)

This is not Kind. This is the real BlueField-2 + DPF v26.4.0 path.

### Connect

```bash
# VPN Mac
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile=$HOME/.ssh/<lab-ssh-key> -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 \
  root@<your-lab-hosts>

k3s kubectl get nodes
k3s kubectl get dpuoperatorconfig
k3s kubectl -n dpf-operator-system get pods
k3s kubectl -n dpf-operator-system get dpu
```

### Expected user steps

1. Enable NVIDIA (`spec.vendor=nvidia` or annotation)
2. LCM preflight + dependency check (k8s 1.36, cert-manager, Argo)
3. Observe/claim Helm pin `dpf-operator-system/dpf-operator@v26.4.0`
4. LCM conditions Supported + VendorOperatorReady
5. DPF provisions the DPU (BFB, NVCONFIG, reboot)
6. Dataplane: HBN / flannel / CNI / SFC / SR-IOV

### Last recorded live result (2026-10-25 ~03:30Z)

| Step | Result |
|---|---|
| 1 Enable NVIDIA | `spec.vendor=nvidia`, `adopt-existing=true` |
| 2 Preflight | k3s 1.36.4, cert-manager, Argo, NFD, maintenance-operator present |
| 3 Helm | Secret claimed; adapter owner + mode=adopted; storage owner=helm |
| 4 LCM | `action=noop` `installed=v26.4.0` Supported=True VendorOperatorReady=True |
| 5 DPF provision | **STALL** — DPU `dh2-bluefield-2` phase **Rebooting** |
| 6 Dataplane | `DPFReady=False`; hbn / flannel / cni-installer / sfc **Pending**; sriov-device-plugin Pending ~37h |

DPU annotations / reboot:

- `provisioning.dpu.nvidia.com/host-power-cycle-required=true`
- `rebootMethod` / `rebootStatus.method`: **SystemLevelReset**
- Host agent wants mlxfwreset level 3 on `/dev/mst/mt41686_pciconf0` (and .1)
- Pending NVCONFIG: `PF_SF_BAR_SIZE`, `PF_TOTAL_SF`
- BFB `bf-bundle-340` prepared/ready
- Device `mt2321xz0kvx` PCI `0000-0f-00` serial `MT2321XZ0KVX`

**Not run:** `mlxfwreset`, host reboot, helm-uninstall, AMD stubs.

After the LCM observer patch, the intended live signal is
`VendorDPUReady=False/Rebooting` with the DPU name in the message, while
`VendorOperatorReady` stays True. Image on lab at last roll was
`controller:lcm-20261024b` (observer landed after that roll — roll again
if you want the condition on the live object).

### Compiled LCM tests on dh2

dh2 has no `go`. Mac:

```bash
GOTOOLCHAIN=go1.26.10 GOOS=linux GOARCH=amd64 \
  go test -c -o /tmp/lifecycle.test ./pkg/lifecycle/
```

Copy binary + `go.mod` + `config/rbac/role.yaml` + testdata into
`/tmp/lab-job-<name>/`. Run. Delete job dir.

Recorded: full package PASS after go.mod was included.

---

## 3. Conformance / mapping (related e2e)

```bash
go test ./test/conformance/ -count=1
go test ./pkg/mapping/ -count=1
go test ./pkg/translation/ -count=1
```

Evidence unit log (`evidence/unit-tests.log`) — coverage snapshot:

| Package | Time | Cover |
|---|---|---|
| pkg/lifecycle | 2.015s | 89.2% |
| pkg/mapping | 1.369s | 80.3% |
| pkg/translation | 7.153s | 72.7% |
| pkg/discovery | 0.775s | 80.4% |
| pkg/vsp | 4.665s | 73.0% |
| test/conformance | 11.413s | — |

---

## 4. What “e2e green” does and does not mean

**Green Kind e2e means:** manager, VSP, NVIDIA emit, chart NF, and probe
Helm install/upgrade/rollback/uninstall work on a disposable cluster.

**It does not mean:** the BlueField finished provision. That is step 5 on
dh2 and is still **Rebooting**.

**Green LCM on dh2 means:** pin claimed, noop, operator CRDs/controllers
healthy. Dataplane can still be Pending.

---

## 5. Re-run checklist

```bash
# Mac
export GOTOOLCHAIN=go1.26.10
go test ./pkg/lifecycle/ -count=1
go test -race ./pkg/lifecycle/ -count=1
make govulncheck
make docker-preflight
make test-e2e

# Lab hop
ssh ... root@<your-lab-hosts> \
  'k3s kubectl get dpuoperatorconfig dpu-operator-config -o yaml | head
   k3s kubectl -n dpf-operator-system get dpu dh2-bluefield-2 -o yaml | head
   k3s kubectl -n dpf-operator-system get dpuservice'
```
