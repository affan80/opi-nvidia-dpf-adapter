# Approach and what is needed

## Flow

```
ParseIntent → RunPreflight → Installer.Ensure → EvaluatePolicy → AssessReadiness
```

| Unit | File | Does |
|---|---|---|
| intent | `pkg/lifecycle/intent.go` | spec.vendor, else annotation; sanitize token |
| preflight | `pkg/lifecycle/preflight.go` | skip empty; reject unknown (installer not called) |
| installer | `pkg/lifecycle/installer.go` | detect `DPU` or `DSCDevice` via REST mapper |
| policy | `pkg/lifecycle/policy.go` | `ClassifySkew` against `[v26.4.0, v27.0.0)` |
| readiness | `pkg/lifecycle/readiness.go` | boolean for logs; **not** the Ready condition |
| ensure | `pkg/lifecycle/ensure.go` | orchestrates the five units |
| reconcile | `pkg/lifecycle/lifecycle_controller.go` | Get config → EnsureInstalled → set `VendorOperatorSupported` |

Contract updates in the same change:

- `docs/hybrid-integration-design.md` install step 5
- `docs/lcm-ensure-installed.md`
- `README.md` tests + lab status

## What is needed

| Need | Why |
|---|---|
| Go 1.26 (go.mod) | CI and local `make lint` |
| `DpuOperatorConfig` with `spec.vendor` enum | intent parsing |
| DPF already on the cluster | detect, do not helm-install |
| Version pin annotation (optional) | policy when discovery has no image read |
| F5 VPN + `opi_lab_key` on the VPN Mac | reach 172.22.1.2 |
| `/tmp/lab-job-<name>/` for image import | lab rule: no `/etc`, no packages |

## What must not be used

| Forbidden | Why |
|---|---|
| `helm upgrade` / `helm uninstall` DPF | later phase; lab rule |
| Apply AMD stub CRDs on dh2 | fake device on BlueField |
| LCM writes `Ready` | daemon owns it |
| Copy `opi_lab_key` / install F5 on another Mac | privacy |
| Change dh2 `/etc`, packages, systemd | servers are for use, not changes |
