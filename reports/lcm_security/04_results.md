# Results

Date: 2026-10-24 (re-run same day). SSH to dh2 OK (`hostname=dh2`).
VPN already Connected. Full `pkg/lifecycle` binary **PASS** on dh2 again.

## Passed

| Check | Where | Result |
|---|---|---|
| `go test ./pkg/lifecycle/` | this Mac | ok 0.928s |
| `go test ./pkg/mapping/` | this Mac | ok 1.709s |
| `go test -race ./pkg/lifecycle/` | this Mac | ok 2.779s |
| `go vet ./pkg/lifecycle/` | this Mac | pass |
| Named LCM/RBAC/upgrade/cleanup tests | **dh2** | all PASS (see list below) |
| Full `pkg/lifecycle` package | **dh2** | PASS |
| `go mod verify` | this Mac | all modules verified |
| Live Helm `dpf-operator` | dh2 | `owner=helm` (external) |
| Live ClusterRole wildcards | dh2 | none |
| LCM conditions | dh2 | Supported=True, Ready=True |
| Adapter pods | dh2 | manager + VSP Running |

Named tests that **PASS** on dh2 include: fresh/already-installed Helm,
unmanaged not adopted, supported upgrade, same-version no-op,
unsupported/malformed reject, downgrade policy, timeout/unknown/retry,
successful and failed rollback, controller/adopted/external cleanup,
missing/conflict fail-safe, shared CRDs and dataplane, namespace
scope, ownership race, idempotent cleanup, install failure and
recovery, canceled context and deadline, restart last-known-good,
concurrent upgrade+cleanup, invalid removal policy, RBAC no
wildcards / no CRD delete / LCM observe verbs, artifact pin, secret
redaction.

## Failed (tracked)

| Check | Result |
|---|---|
| `make govulncheck` | exit 3 — 43 call-graph hits from Go 1.26 stdlib, `golang.org/x/net` v0.49.0, `google.golang.org/grpc` v1.79.3, `go.opentelemetry.io/otel` v1.40.0, `golang.org/x/text` v0.33.0 |

Accepted exception: do not bump `x/net` to v0.60 or change the module
`go 1.26.0` line until k8s v0.36.0 is re-tested. Prefer a patched
Go 1.26 toolchain (lab k3s reports build Go 1.26.7; that is k3s,
not a host compiler).

## Not run / blocked

| Check | Why |
|---|---|
| `make test-e2e` (Kind) | Not executed. Kind is local, not on dh2. |
| `make lint` (golangci + verify-rbac) | **PASS** this run (`0 issues`, markers up to date). |
| `make verify-manifests` | **PASS** this run (role.yaml matches controller-gen). |
| CodeQL | Not added. Org default scanning is the intended path. |
| Live ClusterRole apply | **Not applied.** Generated role is still newer than the running role. |
| helm-uninstall / Helm in main.go | Forbidden / still nil. |

## Live ClusterRole vs generated

Running `opi-nvidia-dpf-adapter-manager-role` is missing:

- `dpuoperatorconfigs` **update** (needed for the cleanup finalizer)
- `dpuoperatorconfigs/finalizers` update
- `operator.dpu.nvidia.com/dpfoperatorconfigs` get/list/watch
- `provisioning.dpu.nvidia.com/dpuclusters` get/list/watch

It already has deployments get/list/watch, CRD get/list, and no `*`.
NeedsKamaji treats a list Forbidden as “Kamaji not required” (safe).
Deploy the regenerated `config/rbac/role.yaml` when you next roll
the adapter image.

## Lab left unchanged

- No helm-upgrade / helm-uninstall
- DPF image still `nvcr.io/nvidia/doca/dpf-system:v26.4.0`
- DPU `dh2-bluefield-2` still **Rebooting** (no firmware reset)
- Job dir `/tmp/lab-job-lcm-sec` deleted
