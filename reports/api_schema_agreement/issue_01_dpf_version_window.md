Issue 1 — DPF support window was v25, lab runs v26.4.0

Date: 2026-10-22
Host: dh2

What we saw
- Adapter code said NVIDIA is supported for [v25.1.0, v26.0.0).
- dh2 is running nvcr.io/nvidia/doca/dpf-system:v26.4.0 (Helm chart dpf-operator-v26.4.0).
- The lifecycle manager would have marked that install TooNew.

Approach
- Read the live operator image and Helm labels first.
- Compare mapping fields to the v26.4 CRD fixtures before changing the window.
- Pin only the release we actually compared.

Fix
- Changed supportedVersions["nvidia"] from [v25.1.0, v26.0.0) to [v26.4.0, v27.0.0)
  in opi-nvidia-dpf-adapter/pkg/lifecycle/version.go.
- Copied the same window into config/versions.yaml (dpf.release = v26.4.0).
- Updated tests so v26.4.0 is Supported and v25.4.0 is TooOld:
  pkg/lifecycle/version_test.go
  pkg/lifecycle/lifecycle_controller_test.go
  test/conformance/dimensions_test.go
  docs/vendor-integration.md
- After this fix, a cluster running DPF v26.4.0 reports VendorOperatorSupported=True.

Test
- make verify-api pass
- go test ./pkg/lifecycle/ pass
- ClassifySkew(nvidia, v26.4.0) = Supported
- ClassifySkew(nvidia, v25.4.0) = TooOld
