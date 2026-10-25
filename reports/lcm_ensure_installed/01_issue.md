# What the issue was

## The blocker (TASK 02.01)

The Lifecycle Manager watched `DpuOperatorConfig` and stopped at a log:

```
would install and pin the vendor operator (placeholder for Helm/OLM)
  vendor=nvidia action=ensure-installed-and-pinned
```

There was no `EnsureInstalled` function, no installer interface, and no
way to test “did Reconcile attempt an install flow?” The Helm/OLM work
had nowhere to land except another log line.

## Two rows that look like bugs (they are the fix)

### installer AMD = absent (no DSC CRD)

**Not a missing install.** `DetectingInstaller` looks up
`dscdevices.dpu.amd.com` / kind `DSCDevice`. On dh2 that CRD is gone on
purpose.

| If we “fixed” it by applying AMD stubs on dh2 | What goes wrong |
|---|---|
| Adapter can emit `DSCDevice` / `DSCFirmware` | Fake Pensando objects on a BlueField card |
| Operators see AMD APIs | They look real; there is no DSC |
| Mapping `when` serial gate can be bypassed | Stub serials become a device inventory |

The secure fix is: **report absent**, keep AMD stubs only under
`test/conformance/crds/amd/` for envtest, keep AMD **Unpinned** in
`version.go`. dh1 is the Pensando host; this adapter is not running there.

### LCM wrote Ready = no

**Not a missing condition.** `DpuOperatorConfig` printer column and the
dpu-operator daemon own `Ready`. If the LCM also upserted `Ready`:

- two writers on one type
- condition flaps every reconcile
- `kubectl get dpuoperatorconfig` lies about cluster readiness

The LCM’s type is `VendorOperatorSupported`. In-memory
`AssessReadiness.Ready` is a boolean on the EnsureInstalled result (API
present + version in window). It is never written as condition type
`Ready`.

## How each piece was fixed

| Issue | Root cause | Fix |
|---|---|---|
| Placeholder log only | Reconcile inlined vendor + ClassifySkew + log | `EnsureInstalled` in `pkg/lifecycle/ensure.go`; Reconcile calls it |
| Untestable install | No `Installer` | Interface + recording installer in tests |
| Helm risk | “ensure” sounded like helm upgrade | `DetectingInstaller` only REST-maps; no Helm client |
| AMD “missing” | People expect DSC CRDs everywhere | Probe AMD, return `absent`; do not apply stubs on BlueField |
| Ready “missing” | People expect LCM to write Ready | Write `VendorOperatorSupported` only; tests fail if Ready appears |
