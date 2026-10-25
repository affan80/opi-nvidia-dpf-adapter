Issue 1 — DPUDeployment would own the same DPF children

Date: 2026-10-22
Host: dh2

What the issue was
- Two legal DPF models exist for a service chain:
  1. Create DPUService + DPUServiceInterface + DPUServiceChain yourself
  2. Create a DPUDeployment and let its controller create those three
- The adapter already emitted DPUService from ServiceFunctionChain
- DPUDeployment CRD on dh2 (created 2026-10-20) says:

    This object connects DPUServices with specific BFBs and
    DPUServiceChains.

    spec.serviceChains: configuration related to the DPUServiceChains
    that the DPUDeployment creates.

- Required on the live CRD: spec.dpus and spec.services
- Extra required under spec.dpus: dpuSetStrategy, nodeEffect
- Those DPU fields are already produced by the DataProcessingUnit mapping
  (DPUDevice, DPUFlavor, BFB, DPU). A DPUDeployment-led SFC mapping
  would be a second owner of BFB/Flavor/DPUSet children.

Live evidence (server dry-run, then deleted /tmp/lab-job-sfc-ownership)

    The DPUDeployment "hbn-chain" is invalid:
    * spec.dpus.dpuSetStrategy: Required value
    * spec.dpus.nodeEffect: Required value

What we changed
- Chose the direct model. Documented in the mapping description.
- TestSFCMappingIsDirectOwnership fails if the SFC mapping emits
  DPUDeployment, DPUServiceTemplate, or DPUServiceConfiguration.
- TestNoTwoMappingsOwnTheSameDPFKind fails if DataProcessingUnit and
  ServiceFunctionChain both emit the same group/kind.

How we fixed it
- One owner per generated CR: the OPI ServiceFunctionChain, via the
  translation controller (controller ownerRef in the same namespace,
  annotation + finalizer across namespaces).
- DPF controllers keep their descendants (ServiceInterfaceSet,
  ServiceChainSet, Helm releases). We do not SSA those.
- DPUDeployment is not generated. Provisioning stays on the DPU mapping.

How to verify
    go test ./pkg/mapping/ -run 'TestSFCMappingIsDirectOwnership|TestNoTwoMappingsOwnTheSameDPFKind'
    # on dh2, read-only:
    kubectl get crd dpudeployments.svc.dpu.nvidia.com \
      -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.description}'
