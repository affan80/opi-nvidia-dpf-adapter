Live API/schema re-test on dh2 — 2026-10-23

VPN: F5 Access Connected (172.22.100.23). k3s v1.36.4+k3s1.
Fixture gate: make verify-api PASS (mappings match vendored CRDs).
Live gate: 1 mapping write is dead on the lab and is failing reconcile now.

================================================================
ERROR 1 (live, blocking) — dead field the mapping still writes
================================================================

  DPUService.spec.security.privileged

  Fixture v26.4: YES
  Live CRD (created 2026-09-07): NO
  Dry-run:  strict decoding error: unknown field "spec.security"
  Without the field: server dry-run CREATE succeeds

  Translator log (repeating ~every 16m):
    apply DPUService/hbn: failed to create typed patch object
    (default/hbn; svc.dpu.nvidia.com/v1alpha1, Kind=DPUService):
    .spec.security: field not declared in schema

  Consequence
    ServiceFunctionChain default/sfc-chart-probe exists (chart hbn).
    Zero DPUService / DPUServiceInterface / DPUServiceChain children.
    The cleanup finalizer is on the SFC. Translation of that SFC is dead
    on this cluster until the mapping stops writing spec.security or the
    live CRD is upgraded (this repo must not upgrade lab CRDs).

================================================================
DEAD APIs confirmed (mapping does NOT write these — correct)
================================================================

  DpuOperatorConfig.spec.vendor
    live NO, fixture NO
    dry-run: unknown field "spec.vendor"
    Enablement stays on annotation lifecycle.opi.nvidia.com/vendor

  DPU.spec.astraEnabled
    live NO, fixture YES
    dry-run: unknown field "spec.astraEnabled"
    Mapping does not write it. Safe.

  DPUDevice.spec.cluster
    live NO, fixture YES
    dry-run: unknown field "spec.cluster"
    Mapping does not write it. Safe.

  DPUDevice.spec.bmcCredentialSecretName
    live NO, fixture YES
    Mapping does not write it. Safe.

  ServiceFunctionChain.status.conditions
    live NO, fixture NO  (status is empty object)
    dry-run status patch: Warning: unknown field "status.conditions"
    Mapping status.schema=pruned — skip Status().Update. Correct.

================================================================
NOT dead — mapping paths that exist on both live and fixture
================================================================

  DataProcessingUnit: spec.nodeName, spec.dpuProductName, status.conditions
  SFC: spec.networkFunctions[].chart.name (PR #6 fields ARE on lab)
  DPUDevice.spec.serialNumber
  DPUFlavor.spec.dpuMode
  BFB.spec.url
  DPU.spec.{dpuNodeName,dpuDeviceName,dpuFlavor,bfb,serialNumber,nodeEffect.noEffect}
  DPUService.spec.{serviceID,helmChart.source.{repoURL,chart,version},interfaces,paused}
  DPUServiceInterface.spec.template...
  DPUServiceChain.spec.template.spec.template.spec.switches
  All amd-dsc200.yaml to: paths (against the stub CRDs installed 2026-10-20)

================================================================
Dead as a real vendor API — AMD on dh2
================================================================

  dscdevices / dscprofiles / dscfirmwares / dscnodepolicies exist
  (created 2026-10-20). Spec keys match the repo stubs, not a published
  AMD operator. Dry-run DSCDevice succeeds against the stub.
  There is still no real AMD DSC operator on this host.

================================================================
Present on lab, correctly NOT emitted (second-owner APIs)
================================================================

  DPUDeployment, DPUServiceTemplate, DPUServiceConfiguration
  (CRDs created 2026-10-20). Direct SFC model must not SSA these.

================================================================
Live objects
================================================================

  DPU / DPUDevice / BFB / DPUFlavor  dh2-bluefield-2  — present
  DPU + BFB phase Initializing
  DataProcessingUnit dh2-bluefield-2  BlueField-2
  DpuOperatorConfig dpu-operator-config
  SFC sfc-chart-probe — no children (blocked by ERROR 1)
  DPUService count: 0

================================================================
What is NOT a lab change
================================================================

  Do not helm-upgrade DPF CRDs. Do not delete CRDs.
  Job scratch was /tmp/lab-job-schema-agree/ and was deleted.

If SFC must work on this lab, drop spec.security.privileged from the
mapping (or SSA-omit when the live schema lacks it). That contradicts
"align to v26.4 fixtures" until the live CRD is replaced by someone
who owns DPF.
