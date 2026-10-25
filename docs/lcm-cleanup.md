# Protect externally owned installations

## Issue

Deleting `DpuOperatorConfig` could have cleaned up whatever sat in
the current reconcile scope: the live Helm `dpf-operator` release,
shared DPF CRDs, or dataplane objects another tenant or OPI object
still needs.

The lab `dpf-operator` secret is `owner=helm` until
`adopt-existing=true` claims the pin as `mode=adopted`. Helm storage
`owner=helm` is not a conflict with the adapter owner label. Adopted
cleanup still defaults to retain. Do not helm-uninstall DPF.

## Fix

`CleanupInstallations` on `LifecycleManager` (called from
`reconcileCleanup` when the cleanup finalizer is holding a deleting
`DpuOperatorConfig`).

| Ownership | How it is decided | Cleanup |
|---|---|---|
| `controller` | `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter` and `mode=installed` | Delete the release only if `removal-policy=delete-owned` |
| `adopted` | adapter owner, mode empty or `adopted` | Follow `adoption-policy` (default retain) |
| `external` | Helm `owner=helm` or any other owner | Preserve |
| `invalid` | missing labels, or adapter owner plus `owner=helm` | Preserve and record failure |

Further gates:

- Shared CRDs stay while another `DpuOperatorConfig` or OPI/DPF
  object still exists. Production never sets `AllowCRDDeletion`.
- Dataplane objects stay if another namespace, another owner, or an
  OPI object still references them.
- Lookups are `namespace/kind/name`. Same name in another namespace
  is not deleted.
- The release is re-read immediately before Uninstall. If ownership
  changed, cleanup fails and preserves.
- Every retry classifies again. Missing Helm client cannot verify
  ownership and will not delete.

`VendorOperatorCleanup` records deleted / preserved / failed /
already-gone. Logs use `SanitizeLogValue`. The LCM still does not
write `Ready`.

## Commands

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestControllerOwned|TestAdoptedRelease|TestExternallyOwned|TestMissingOrConflicting|TestSharedCRDs|TestSharedDataplane|TestNamespaceScoped|TestOwnershipChange|TestCleanupIsIdempotent|TestCleanupFailures|TestCleanupReconcile|TestHelmUninstallRefuses'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

## Lab result (2026-10-24)

VPN Connected. SSH to dh2 OK. Helm secret
`sh.helm.release.v1.dpf-operator.v1` is `owner=helm` — external.
Cleanup tests compiled `linux/amd64` and ran on dh2: every
definition-of-done case **PASS**. No helm-uninstall was issued.
LCM remains `VendorOperatorSupported=True/Supported` and
`VendorOperatorReady=True/Ready`.
