Issue 5 — Live DPU CRD requires spec.bfb; v26.4 fixture uses a one-of

Date: 2026-10-22
Host: dh2

What we saw
- Live DPU CRD (installed 2026-09-07) required list includes spec.bfb.
- Vendored v26.4 fixture: exactly one of bfb or blueFieldSoftware.
- Fixture also has spec.astraEnabled. Live CRD does not.
- Live DPUDevice is also older (no cluster / bmcCredentialSecretName).

Approach
- Compare required fields on both schemas to what the mapping already writes.
- The mapping writes spec.bfb, so apply works on both the old live CRD and the fixture.

Fix
- Adapter-side fix: keep writing DPU.spec.bfb from the BFB name annotation.
  That satisfies the old live CRD (bfb required) and the v26.4 fixture
  (exactly one of bfb or blueFieldSoftware). File:
  config/mappings/dataprocessingunit.yaml
      - to: spec.bfb
        cel: source.metadata.?annotations['dpu.nvidia.com/bfb'].orValue('bf-bundle')
- Pin DPF to v26.4.0 in config/versions.yaml after that check.
- Gate: TestMappingFieldsExistInVendoredCRDs checks spec.bfb exists on the fixture.
- Not fixed on dh2: did not replace the Sept 7 DPU CRD (astraEnabled still absent).
  No need to. Apply already works.

Test
- DPU/DPUDevice/BFB on dh2 still apply (real serial present, phase Initializing).
- Initializing is not this schema mismatch. DPFOperatorConfig is empty.
- make test pass
