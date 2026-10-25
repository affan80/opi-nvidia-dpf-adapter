# Results — 2026-10-24

## TDD

First `go test` failed to compile (`undefined: SubscriptionGVK`,
`undefined: NVIDIAOLMPublished`, `InstallConfig` had no OLM fields).
That was the red step. After the observer landed, the same `-run`
passed.

## Local

| Check | Result |
|---|---|
| OLM-focused `go test ./pkg/lifecycle/` | ok |
| `go test ./pkg/lifecycle/ ./pkg/mapping/` | ok |
| `go test -race ./pkg/lifecycle/` | ok |
| `make verify-manifests` | ok (after markers on the reconciler) |
| Kind e2e | **5/5 PASS**, cluster deleted |
| `pkg/vsp` (unrelated unix-socket flake in sandbox) | ok with full permissions |

## Lab dh2 (`<your-lab-hosts>`)

VPN Connected. k3s `v1.36.4+k3s1`.

| Check | Got | Want |
|---|---|---|
| `subscriptions.operators.coreos.com` | NotFound | NotFound |
| `clusterserviceversions.operators.coreos.com` | NotFound | NotFound |
| Subscription / CSV objects | none | none |
| Helm `dpf-operator` secret | `owner=helm` | unchanged |
| LCM conditions | Supported=True, Ready=True | unchanged |
| Compiled OLM tests on dh2 | PASS | PASS |
| Subscriptions after the tests | still none | still none |
| `/tmp/lab-job-olm/` | deleted | deleted |

No OLM install. No helm-uninstall. No AMD stubs. No firmware reset.

## Versions

| Pin | Value |
|---|---|
| Go | 1.26.0 / toolchain go1.26.10 |
| Kubernetes modules | v0.36.0 |
| k3s | v1.36.4+k3s1 |
| DPF | Helm `dpf-operator` v26.4.0 |
| OLM package | unpublished |
