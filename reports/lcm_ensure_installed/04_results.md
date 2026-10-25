# Results — 2026-10-23

## Unit (`go test ./pkg/lifecycle/ -count=1`)

PASS. `make lint`: 0 issues.

| Test | Result |
|---|---|
| `TestReconcileCallsEnsureInstalled` | PASS — one Ensure call, nvidia / v26.4.0 / spec |
| `TestParseIntent` | PASS — spec wins, annotation fallback, injection → invalid |
| `TestRunPreflight` | PASS |
| `TestDetectingInstaller` present / absent / no Helm | PASS |
| Skip empty vendor / skip `acme` | PASS — installer not called |

## Lab matrix (13/13)

| Check | Got | Want |
|---|---|---|
| intent.vendor | nvidia | nvidia |
| intent.source | spec | spec |
| intent.pin | v26.4.0 | v26.4.0 |
| preflight | ok | ok |
| installer NVIDIA | detected | detected |
| installer AMD | **absent** | **absent** |
| DPF image | `dpf-system:v26.4.0` | same |
| policy.skew | Supported | Supported |
| readiness (in-memory) | true | true |
| `VendorOperatorSupported` | True/Supported | True/Supported |
| LCM wrote `Ready` | **no** | **no** |

## Live Reconcile after import

Old: `would install ... action=ensure-installed-and-pinned`

New:

```
ensure installed vendor=nvidia source=spec action=detected
  installed=v26.4.0 skew=Supported ready=true
```

| Path | Log / condition |
|---|---|
| no vendor | `nothing to install`, no condition |
| annotation `acme` | `action=skipped skew=UnknownVendor ready=false`, `False/UnknownVendor` |

Helm on DPF was not run. `/tmp/lab-job-ensure/` deleted after import.
Temp skip/acme configs deleted.
