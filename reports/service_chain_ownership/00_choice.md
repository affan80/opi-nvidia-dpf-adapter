Service-chain ownership — choice

Date: 2026-10-22
Skill: /Superpowers (brainstorming then start working on this)
Host: dh2

Brainstorm
1. Direct DPUService + DPUServiceInterface + DPUServiceChain
   - Translation controller is the only owner of those three CRs
   - Same namespace as the OPI ServiceFunctionChain (else dpf-operator-system)
   - DPF controllers then own descendants (ServiceInterfaceSet, Helm, …)
   - Live dry-run: Interface and Chain admitted on dh2
2. DPUDeployment-led
   - Adapter would emit DPUDeployment + DPUServiceTemplate + DPUServiceConfiguration
   - DPF's DPUDeployment controller creates DPUService, Interface, Chain, and DPUSets
   - CRD text on dh2: "ServiceChains contains the configuration related to
     the DPUServiceChains that the DPUDeployment creates"
   - Requires spec.dpus (BFB, Flavor, dpuSetStrategy, nodeEffect) already
     owned by the DataProcessingUnit mapping
   - Live dry-run rejected: spec.dpus.dpuSetStrategy and spec.dpus.nodeEffect required
3. Both
   - Two controllers write the same DPUService names. Forbidden.

Choice
Direct DPUService + DPUServiceInterface + DPUServiceChain.
Never emit DPUDeployment, DPUServiceTemplate, or DPUServiceConfiguration.

Unknown input
- Image-only network functions: skipped (no DPF image field)
- Image+chart together: rejected by OPI CRD CEL (PR #6)
- A mapping that adds DPUDeployment: TestSFCMappingIsDirectOwnership fails

Done when
Written resource graph has an owner and namespace for each generated CR.
Locked in config/mappings/servicefunctionchain.yaml and
pkg/mapping/ownership_test.go.

Reports
- issue_01_dpudeployment_would_own_children.md
- issue_02_mapping_only_emitted_dpuservice.md
- issue_03_cel_wrappers_block_apply.md
