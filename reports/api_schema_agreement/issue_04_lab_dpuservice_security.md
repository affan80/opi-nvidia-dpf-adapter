Issue 4 — Live DPUService CRD on dh2 has no spec.security

Date: 2026-10-22
Host: dh2

What we saw
- Mapping writes spec.security.privileged = true.
- Vendored v26.4 fixture has that field. CEL requires it when deployInCluster is false.
- The DPUService CRD installed on 2026-09-07 does not have spec.security.
- Helm take-ownership of dpf-operator v26.4.0 labelled the old CRD and did not replace the schema.
- The apiserver will prune privileged on this cluster.

Approach
- Dump live CRDs and compare property paths to fixtures before pinning.
- Do not upgrade CRDs on the lab server. This adapter does not own them.

Fix
- Adapter-side fix: keep writing spec.security.privileged = true so kind and
  conformance (v26.4 fixtures) stay valid. File:
  config/mappings/servicefunctionchain.yaml
- Gate: TestMappingFieldsExistInVendoredCRDs fails if that field disappears
  from the fixtures.
- Documented the live-cluster prune in docs/api-versions.md.
- Not fixed on dh2: we did not replace the Sept 7 DPUService CRD. Lab servers
  are not changed from this repo. Whoever owns DPF must upgrade that CRD
  before privileged survives apply on the lab.
- Check later:
  kubectl get crd dpuservices.svc.dpu.nvidia.com -o yaml | grep privileged

Test
- Fixture agreement: TestMappingFieldsExistInVendoredCRDs pass
- Live CRD: field still missing on dh2 (expected until DPF CRDs are upgraded by whoever owns them)
