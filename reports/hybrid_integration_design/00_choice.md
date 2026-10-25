Hybrid integration design — choice

Date: 2026-10-22
Skill: /Superpowers
Host: unit tests (mapping / translation / lifecycle / conformance green)

Brainstorm
1. Keep decisions in six existing docs (api-versions, multi-vendor,
   mapping-spec, mappings YAML, condition-writers, README)
   - Reviewers have no one file to approve. Next irreversible step
     (Helm DPF install, CRD upgrade, DPUDeployment) has no contract.
2. Write a long architecture review
   - TASK 01.06 asked for a short spec, not a second study-notes.md
3. One short design document that records the six required topics and
   is tested against the pins

Choice
docs/hybrid-integration-design.md is the single implementation contract.
Tests fail if a required section or a pin (version, annotation, chain
kind, writer, uninstall) is dropped.

Unknown input
- Mapping emits DPUDeployment, writes Ready, or two vendors share a
  condition: existing tests fail, and the spec names that reject
- spec.vendor on DpuOperatorConfig: apiserver unknown field
- Vendor not nvidia/amd: UnknownVendor

Done when
The design file exists, README points at it, TestHybridDesignDocIsTheContract
passes.

Reports
- issue_01_no_design_doc.md
- issue_02_scattered_contracts.md
