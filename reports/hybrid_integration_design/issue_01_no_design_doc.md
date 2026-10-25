Issue 1 — TASK 01.06 blocker: no design document

Date: 2026-10-22

What the issue was
- TASK 01.06 BLOCKER: write and approve a short spec before irreversible
  implementation
- DONE WHEN: one reviewed design document is the single implementation
  contract
- FILE: docs/hybrid-integration-design.md — did not exist
- docs/ listed api-versions, mapping-spec, multi-vendor, vendor-integration,
  vsp, study-notes. None of those is the contract that records all six
  required topics in one place
- Code and mappings had already moved (pins, annotations, direct chain,
  condition writers) without that file. Further steps (LCM Helm install,
  live CRD upgrade, DPUDeployment) would be irreversible without a spec

What we changed
- Added docs/hybrid-integration-design.md
- Sections: Version constraints, Install strategy, Config annotations,
  Service-chain resource graph, Status ownership, Uninstall policy,
  Rejection summary
- README links it as the implementation contract
- pkg/mapping/hybrid_design_test.go fails if the file is missing, if a
  section heading is gone, or if pins from versions.yaml / writer table /
  SFC mapping are not in the text

How we fixed it
- The spec is now the contract. Changing a pin requires changing the
  document in the same change, or CI fails.

How to verify
    test -f docs/hybrid-integration-design.md
    go test ./pkg/mapping/ -run TestHybridDesignDocIsTheContract
    go test ./pkg/mapping/ ./pkg/translation/ ./pkg/lifecycle/ ./test/conformance/ -count=1
