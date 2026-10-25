# Fix

TDD: failing tests first (`SubscriptionGVK`, `ClusterOLMClient`,
`LifecycleManager.OLM` undefined), then the observer.

| File | Change |
|---|---|
| `pkg/lifecycle/olm_client.go` | **new** `OLMClient`, `MemoryOLMClient`, `ClusterOLMClient`, `OLMInstaller` |
| `pkg/lifecycle/olm_client_test.go` | **new** absent / observe / no-create / adopt-gate / Helm-wins / main wiring |
| `pkg/lifecycle/config.go` | OLM fields stay empty; catalog/package/subscription annotations rejected |
| `pkg/lifecycle/ensure.go` | Helm → OLM → DetectingInstaller. Helm storage wins. |
| `pkg/lifecycle/lifecycle_controller.go` | field `OLM`; RBAC markers get/list/update/patch Subscriptions, get/list CSVs. No create/delete. |
| `cmd/main.go` | `OLM: NewClusterOLMClient(client, RESTMapper)` |
| `config/versions.yaml` | `dpf.olm.published: false`, empty package/channel/catalog |
| `config/rbac/role.yaml` | generated from the new markers |
| `docs/lcm-olm.md` | in-repo record |
| `docs/hybrid-integration-design.md` | contract: OLM observe-only, Helm wins |

## How it works

`ClusterOLMClient` reads unstructured Subscription + CSV. It does not
import `github.com/operator-framework` and does not shell out.

`OLMInstaller.Ensure`:

1. AMD → `skipped`.
2. OLM APIs absent → `ErrOLMNotPresent` → DetectingInstaller.
3. APIs present, no Subscription → do not create (`ErrOLMPackageNotPublished`).
4. Subscription exists, no `adopt-existing` → `unmanaged`, version from CSV.
5. Matching pin + `adopt-existing=true` → stamp owner + `mode=adopted`.
6. Already owned → `noop`.

`Subscribe` always returns `ErrOLMMutationNotConfigured`.

Identity when a CSV exists:

```
dpf-operator-system/dpf-operator@v26.4.0
```
