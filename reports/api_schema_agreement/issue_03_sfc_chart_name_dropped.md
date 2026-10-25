Issue 3 — ServiceFunctionChain mapping dropped the Helm chart name

Date: 2026-10-22
Host: dh2 (schema compare; no ServiceFunctionChain object on the cluster)

What we saw
- OPI ServiceFunctionChain requires networkFunctions[].chart.name.
- DPF DPUService has spec.helmChart.source.chart.
- The mapping copied repository and version only.
- Helm would get a URL and a version with no chart identity.

Approach
- Walk every mapping to: and from: path against the vendored CRDs.
- That walk is what showed the missing chart name.

Fix
- Added this field to config/mappings/servicefunctionchain.yaml:

      - to: spec.helmChart.source.chart
        cel: item.chart.name
        required: true

- Same field added to test/conformance/testdata/servicefunctionchain-xns.yaml.
- Tests now fail if the chart name is missing:
  - pkg/translation/servicefunctionchain_controller_test.go
  - test/conformance/conformance_test.go (wantFields)
  - test/e2e/e2e_test.go
- Mapping on dh2 is not redeployed yet. Apply the ConfigMap to make this live.

Test
- make test pass (mapping, translation, conformance)
- TestMappingFieldsExistInVendoredCRDs pass
- Kind e2e not run (Docker Desktop down)
