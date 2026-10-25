Issue 6 — ServiceFunctionChain status schema is empty, so DPFReady is pruned

Date: 2026-10-22
Host: dh2

What we saw
- Installed OPI ServiceFunctionChain CRD matches the vendored fixture.
- status is an empty object. No conditions[].
- The mapping still writes DPFReady onto the source.
- The apiserver deletes that write. Conformance already marks this statusPruned.

Approach
- Treat this as an upstream OPI schema gap, not an adapter inventing a new status field.
- Keep the mirror rule so it activates when the CRD gains conditions.

Fix
- Adapter-side fix: keep the DPFReady mirror on ServiceFunctionChain so it
  starts working the day upstream adds status.conditions. File:
  config/mappings/servicefunctionchain.yaml (status.conditions type DPFReady)
- Do not invent a custom SFC status field. The apiserver would prune it.
- OPI pin stays config.openshift.io/v1 @ c107ef719f74d4f841ceb1ed45bbd293b3739a3c
  in config/versions.yaml.
- DataProcessingUnit already has status.conditions, so DPFReady on that
  object is written and kept (False while DPU/BFB are Initializing).
- Not fixed upstream: SFC CRD status is still empty. That change belongs in
  opiproject/dpu-operator, not this adapter.

Test
- OPI fixture vs live CRD: property paths match
- make test / conformance pass (SFC case does not assert status)
