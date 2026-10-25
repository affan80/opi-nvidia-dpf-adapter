Issue 2 — contracts lived in six places and could contradict

Date: 2026-10-22

What the issue was
- Version window: config/versions.yaml + pkg/lifecycle/version.go +
  docs/api-versions.md
- Enablement: lifecycle_controller.go + sample YAML
- Chain graph: servicefunctionchain.yaml comments + ownership tests
- Status: condition-writers.yaml + multi-vendor.md
- Uninstall: Makefile + translation finalizer, never written as policy
- README still said SFC → DPUService only, while the mapping emits
  Interface + Chain
- A reviewer could approve one file and miss a conflict (e.g. emit
  DPUDeployment while the mapping forbids it)

What we changed
- The design doc restates each contract once, with a table pointing at
  the machine-checkable file
- README diagram updated: SFC → DPUService + DPUServiceInterface +
  DPUServiceChain
- TestHybridDesignDocIsTheContract reads versions.yaml, the writer
  table, and the SFC mapping and requires those strings in the spec

How we fixed it
- Drift is a failed test, not a stale paragraph. Uninstall policy is
  explicit: delete OPI sources to GC children; make undeploy removes
  only the adapter; never helm-uninstall DPF or delete OPI/DPF CRDs
  from this repo.

How to verify
    go test ./pkg/mapping/ -run TestHybridDesignDocIsTheContract
    grep -n 'single implementation contract' docs/hybrid-integration-design.md
