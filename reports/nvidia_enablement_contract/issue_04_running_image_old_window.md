Issue 4 — live manager image still uses the old v25 window

Date: 2026-10-22
Host: dh2

What the issue was
- After apply, VendorOperatorSupported was False / TooNew:
  "nvidia operator v26.4.0 is newer than the supported range
  [v25.1.0, v26.0.0)"
- That window was already fixed in the repo to [v26.4.0, v27.0.0).
- The pod opi-nvidia-dpf-adapter-controller-manager is still the
  previous image (controller:latest from an older build). It has not
  been rebuilt with the new version.go.

What we changed
- No further code change in this pass. The pin is already in
  pkg/lifecycle/version.go and config/versions.yaml.
- The NVIDIA contract is still met: the manifest applied and the LCM
  read vendor=nvidia. TooNew is a stale binary, not a bad contract.

How we fixed it
- Documented. Rebuild and redeploy the manager image on dh2 to get
  Supported=True for v26.4.0. That is an image roll, not a CRD change.
- New tests already classify v26.4.0 as Supported.

Test
- go test ./pkg/lifecycle/ : ClassifySkew(nvidia, v26.4.0) = Supported
- live cluster condition still TooNew until the image is rebuilt
