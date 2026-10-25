Issue 3 — no DpuOperatorConfig existed, so NVIDIA was never selected

Date: 2026-10-22
Host: dh2

What the issue was
- kubectl get dpuoperatorconfig returned an empty list.
- The LCM watches that object. With nothing applied, there is no
  vendor, no skew condition, and no NVIDIA enablement.
- DONE WHEN: a real Kubernetes manifest passes CRD validation and
  unambiguously selects NVIDIA.

What we changed
- Added config/samples/dpuoperatorconfig-nvidia.yaml
  name: dpu-operator-config (the name upstream expects)
  annotations vendor=nvidia, version=v26.4.0
  spec.logLevel: 2 only
- README quick start now applies that sample first.
- docs/vendor-integration.md states the annotation contract and the
  reject rule.

How we fixed it
- Applied the sample on dh2. Created.
- Stored object has:
  - spec: {logLevel: 2}  (no vendor field)
  - annotations vendor=nvidia
- LCM log: "would install and pin" vendor=nvidia
- That is the unambiguous selection. CRD validation passed.

Test
- kubectl apply -f config/samples/dpuoperatorconfig-nvidia.yaml on dh2
- make test pass
