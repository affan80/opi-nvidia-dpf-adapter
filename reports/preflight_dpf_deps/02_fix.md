# Fix

New file `pkg/lifecycle/dependency_checker.go`.

`EnsureInstalled` order is now:

intent → preflight → reject helm overrides → pinned `InstallConfig` →
**CheckDependencies** → installer → readiness

`CheckDependencies` does not install charts. It only reads discovery
and the REST mapper.

| Gate | Fail result |
|---|---|
| No `ServerVersion` | `Blocked`, `cannot reach the Kubernetes API` |
| Kubernetes not minor v1.36 | `Blocked`, names both versions |
| NVIDIA missing cert-manager | `Blocked`, missing `cert-manager` |
| NVIDIA missing Argo CD | `Blocked`, missing `argo-cd` |
| Kamaji needed (`DPUCluster` type=kamaji) and API absent | `Blocked`, missing `kamaji` |
| AMD | access + Kubernetes only |

On failure:

- `VendorOperatorSupported=False` reason `Blocked`
- message names the missing item
- installer is **not** called (`action=skipped`)
- `AssessReadiness.Ready` is false
- LCM still does not write `Ready`

`cmd/main.go` wires a discovery client so a forgotten probe cannot
skip the Kubernetes check.

`config/versions.yaml` now lists `dpf.dependencies.required`
(`cert-manager`, `argo-cd`) and `conditional` (`kamaji`).
