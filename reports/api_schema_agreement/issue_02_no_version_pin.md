Issue 2 — Kubernetes, OPI and DPF versions were not pinned in one place

Date: 2026-10-22
Host: dh2

What we saw
- Live cluster: k3s v1.36.4+k3s1, OPI CRDs config.openshift.io/v1, DPF operator v26.4.0.
- go.mod already used k8s.io/apimachinery v0.36.0.
- The repo had no file that stated those three versions together.
- A later CRD bump could silently break translation.

Approach
- Collect versions from the live cluster first (k3s --version, operator image, CRD list).
- Compare installed OPI CRDs to test/e2e/crds/opi.
- Compare mapping to: paths to test/e2e/crds/dpf.
- Write the pin only after that comparison.

Fix
- Added config/versions.yaml as the single pin:
  - Kubernetes v1.36.4 (k3s, modules v0.36.0)
  - OPI config.openshift.io/v1 @ c107ef719f74d4f841ceb1ed45bbd293b3739a3c
  - DPF v26.4.0, window [v26.4.0, v27.0.0)
- Added a drift gate so the pin cannot rot:
  - pkg/mapping/crd_agreement_test.go
    TestMappingFieldsExistInVendoredCRDs
    TestPinnedVersionsAgree
  - pkg/lifecycle/version_test.go
    TestPinnedNVIDIAWindowMatchesVersionsYAML
  - Makefile target: make verify-api (also wired into make lint)
- Wrote the comparison notes in docs/api-versions.md and the README pin table.

Test
- make verify-api pass
- TestPinnedVersionsAgree pass
- TestPinnedNVIDIAWindowMatchesVersionsYAML pass
