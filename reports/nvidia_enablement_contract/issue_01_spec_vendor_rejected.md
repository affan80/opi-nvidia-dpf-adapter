Issue 1 — spec.vendor cannot select NVIDIA on the live CRD

Date: 2026-10-22
Host: dh2

What the issue was
- Task: choose how to enable NVIDIA. Options were an existing-object
  annotation or a versioned upstream spec.vendor field.
- Upstream DpuOperatorConfig spec only has logLevel. There is no vendor.
- We dry-ran this on dh2:

    spec:
      vendor: nvidia
      logLevel: 2

- The apiserver returned BadRequest:
  DpuOperatorConfig in version "v1" cannot be handled as a
  DpuOperatorConfig: strict decoding error: unknown field "spec.vendor"
- So a schema change in this repo would be a fork. Adding spec.vendor
  locally would not match the installed CRD. The field is not pruned
  silently here; it is rejected.

What we changed
- Chose the existing-object annotation as the contract.
- Did not edit the OPI CRD.
- spec.vendor is still read first in ResolveVendor so a future upstream
  field wins the day it exists.

How we fixed it
- Contract:

    metadata.annotations["lifecycle.opi.nvidia.com/vendor"] = nvidia
    metadata.annotations["lifecycle.opi.nvidia.com/vendor-operator-version"] = v26.4.0
    spec.logLevel = 2

- File: config/samples/dpuoperatorconfig-nvidia.yaml
- File: pkg/lifecycle/lifecycle_controller.go (ResolveVendor)
- Dry-run of that sample on dh2: EXIT 0. spec stored as {logLevel: 2}.
  Annotation nvidia survived.
- Real apply: dpuoperatorconfig/dpu-operator-config created.
  LCM log: vendor=nvidia.

Test
- make test pass
- conformance: "rejects spec.vendor at the apiserver and accepts the NVIDIA annotation"
