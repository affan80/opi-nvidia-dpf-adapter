Leftover dead APIs + vulns — dh2 — 2026-10-23

What was still dead (not DPF CRD skew)
- DpuOperatorConfig.spec.vendor — OPI CRD had only logLevel. Dry-run: unknown field. Enablement was annotation-only.
- ServiceFunctionChain.status.conditions — OPI status was an empty object. Mapping correctly pruned (status.schema: pruned).
- AMD DSC* — stub CRDs installed on dh2 (BlueField host, no Pensando card). Fake-device API.

What changed
- Pinned OPI fixture + live CRD: spec.vendor enum nvidia|amd, maxLength 16, CEL. Annotation kept as fallback.
- LCM SanitizeVendorToken: [a-z][a-z0-9-]{0,15}. Path/URL/shell tokens become "invalid" (UnknownVendor). Raw string is not echoed.
- Pinned OPI SFC fixture + live CRD: status.conditions (no preserve-unknown-fields). Mapping + writer table flipped to schema: conditions. Translator writes DPFReady only.
- AMD: emit/status when requires a non-empty serial; stub CRD minLength+pattern on serial, https-only firmware URL, PCI pattern. Stub CRDs deleted from dh2. Live ConfigMap is NVIDIA-only (amd-dsc200.yaml stays in-repo for envtest).

Vulns closed
- Free-form spec.vendor later driving Helm/OLM (path, URL, shell).
- Silent SFC status write / preserve-unknown dump.
- Empty serial → DSCDevice (fake card).
- file:// firmware URL on the stub.
- Stub AMD APIs on a BlueField host (and the controller watching them).

Tests (local, envtest 1.36)
  go test $(go list ./... | grep -v /e2e)  ok
  including test/conformance (now uses pinned fixtures, not sibling checkouts)

Lab (dh2, F5 Connected, ping 0% loss)
  spec.vendor=nvidia accepted on dpu-operator-config
  spec.vendor=acme / ../../../etc/passwd / nvidia;id  rejected by CRD enum
  annotation vendor=acme → VendorOperatorSupported=False reason=UnknownVendor
  SFC default/sfc-chart-probe status.conditions[DPFReady]=True AllServicesReady
  AMD CRDs: none
  mappings ConfigMap keys: dataprocessingunit.yaml, servicefunctionchain.yaml
  controller + VSP Running; /tmp/lab-job-* cleaned
