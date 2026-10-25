Dead API fix — tested on dh2 — 2026-10-23

Problem
- Mapping writes DPUService.spec.security.privileged (required on v26.4 fixtures).
- Live CRD (2026-09-07) has no spec.security. SSA failed the whole object:
  .spec.security: field not declared in schema
- default/sfc-chart-probe had zero children.

Fix
- Keep the mapping on the fixtures.
- applyOne drops a path the live CRD rejects and retries
  (pkg/translation/dead_fields.go).
- Refreshed adapter ConfigMap + ClusterRole on dh2 (interfaces/chains
  were missing from the 2026-10-20 role). Did not upgrade DPF CRDs.

Lab result
  omitted field not declared in live CRD  field=spec.security  kind=DPUService
  DPUService/hbn created — spec keys: helmChart, interfaces, paused, serviceID
  security: none
  DPUServiceInterface/hbn-ifc  interfaceType=service  interfaceName=net0
  DPUServiceChain/sfc-chart-probe

Still dead (not written; dry-run still rejects)
  spec.vendor, spec.astraEnabled, spec.cluster, status.conditions

Tests
  go test ./pkg/translation/ ./pkg/mapping/ ./test/conformance/  ok
