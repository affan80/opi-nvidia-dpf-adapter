TASK 01.06 approved — 2026-10-23

File
docs/hybrid-integration-design.md

Done when
One reviewed design document is the single implementation contract.

Review
- Six required headings present: Version constraints, Install strategy,
  Config annotations, Service-chain resource graph, Status ownership,
  Uninstall policy
- Pins match config/versions.yaml (K8s v1.36.4, OPI c107ef71 unmerged,
  DPF [v26.4.0, v27.0.0))
- Writer table matches config/condition-writers.yaml
- SFC graph matches config/mappings/servicefunctionchain.yaml
  (DPUService + DPUServiceInterface + DPUServiceChain, never DPUDeployment)
- README links the file as the implementation contract

Tests (2026-10-23, this Mac)
    go test ./pkg/mapping/ ./pkg/translation/ ./pkg/lifecycle/ ./test/conformance/ -count=1
    ok mapping
    ok translation
    ok lifecycle
    ok conformance

Approval
This document is the single implementation contract. Later irreversible
work (Helm/OLM DPF install, live CRD upgrade, DPUDeployment, writing Ready)
is out of scope until this file and config/versions.yaml change together.
