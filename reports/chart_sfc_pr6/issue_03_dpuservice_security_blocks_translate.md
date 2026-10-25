Issue 3 — chart SFC admitted, DPUService apply failed (found while testing)

Date: 2026-10-22
Host: dh2

What the issue was
- After sfc-chart-probe was admitted, no DPUService/hbn existed.
- Manager log:

    apply DPUService/hbn: failed to create typed patch object
    (default/hbn; svc.dpu.nvidia.com/v1alpha1, Kind=DPUService):
    .spec.security: field not declared in schema

- The mapping writes spec.security.privileged because pinned DPF v26.4
  CEL requires it when deployInCluster is false.
- The DPUService CRD on dh2 is the Sept 7 schema. It has no spec.security.
  Helm take-ownership of v26.4 did not replace that CRD.
- This is the same skew as api_schema_agreement issue_04. It blocked
  chart translation on the live cluster.

What we changed
- Did not remove privileged from the shipped mapping. Kind/conformance
  use v26.4 fixtures; that CEL would then reject the DPUService.
- Did not upgrade the lab DPUService CRD (lab servers are not changed
  from this repo).
- Left the mapping gated on 'chart' in item so the OPI side is honest.
- Documented: chart SFC admission works; DPUService emit on dh2 waits
  on the v26.4 DPUService CRD (or a rebuilt mapping overlay that omits
  security, which would then fail kind).

How we fixed it
- For this task (PR #6 / chart input): admission and fixture pin are
  the fix. The security field is a DPF CRD skew, not an OPI chart-field
  lie.
- To get DPUService/hbn on dh2 later: upgrade dpuservices.svc.dpu.nvidia.com
  to the v26.4 schema, or rebuild the manager after a lab overlay that
  drops spec.security.privileged.

Test
- Live: SFC admitted, DPUService missing, log shows .spec.security
- make test (fixtures v26.4): pass with privileged still in the mapping
