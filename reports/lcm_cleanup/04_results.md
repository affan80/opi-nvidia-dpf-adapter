# Results

Date: 2026-10-24. VPN Connected. SSH to dh2 OK.

## Cleanup tests on dh2

All named definition-of-done cases **PASS**:

| Test | Result |
|---|---|
| Controller-owned + `delete-owned` | PASS — release deleted |
| Adopted follows adoption policy | PASS — retain by default; delete when allowed |
| Externally owned (`owner=helm`) | PASS — preserved |
| Missing / conflicting ownership | PASS — fail safe, release stays |
| Shared CRDs with another tenant | PASS — CRDs not deleted |
| Dataplane referenced by OPI | PASS — preserved |
| Same name, other namespace | PASS — peer stays |
| Ownership flip before delete | PASS — preserved |
| Idempotent retry | PASS — already-gone; external never uninstalled |
| Failures and skips recorded | PASS |
| Reconcile preserves external, drops finalizer | PASS |
| Helm Uninstall refuses external | PASS |

Local `go test ./pkg/lifecycle/ ./pkg/mapping/ -count=1` **ok**.
`golangci-lint` **0 issues**.

## Lab inventory (no helm-uninstall)

| Item | Value |
|---|---|
| Helm secret `dpf-operator` | `owner=helm`, no adapter owner |
| LCM | `VendorOperatorSupported=True/Supported`, `VendorOperatorReady=True/Ready` |
| `cmd/main.go` Helm | still nil |

Cleanup of this live release classifies as **external** and is
preserved. Job dir `/tmp/lab-job-cleanup` was deleted.
