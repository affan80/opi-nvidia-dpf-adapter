Issue 2 — unknown vendors were not rejected

Date: 2026-10-22
Host: dh2

What the issue was
- The task requires: validate requests and reject unknown vendors.
- The CRD cannot do that. Annotations are free-form. Dry-run of
  lifecycle.opi.nvidia.com/vendor: acme also EXIT 0.
- Old ClassifySkew("acme") returned Unpinned (Unknown), same as AMD.
  AMD is a known second vendor with no window. acme is not a vendor.
  Treating them the same would accept garbage.

What we changed
- pkg/lifecycle/version.go
  - KnownVendors = nvidia, amd
  - KnownVendor()
  - new skew: UnknownVendor
- pkg/lifecycle/lifecycle_controller.go
  - after ResolveVendor, if !KnownVendor, write
    VendorOperatorSupported=False reason=UnknownVendor
  - do not log "would install and pin"
- Tests:
  - TestKnownVendor (dpf and acme are false)
  - TestLifecycleRejectsUnknownVendor
  - TestLifecycleReportsVersionSkew case "unknown vendor is rejected"
  - TestSkewStatusNeverClaimsFailureForUnknown now includes
    UnknownVendor -> False
  - conformance dimension for acme

How we fixed it
- Known list is the allow-list. nvidia and amd pass. acme, dpf, empty
  after resolve do not.
- AMD still Unpinned (Unknown), not rejected. It is a known vendor.
- Message: vendor "acme" is not known; allowed values are nvidia, amd
- The running manager image on dh2 is still the old binary, so this
  reject is in the repo and in make test, not yet in that pod.

Test
- go test ./pkg/lifecycle/ pass
- make test pass
