# Schema / API catalog

The adapter owns neither OPI nor DPF APIs. It watches unstructured OPI
objects and server-side-applies unstructured DPF objects. Kind-specific
logic lives in `config/mappings/*.yaml`. Pins live in
`config/versions.yaml`. The runtime window is
`pkg/lifecycle/version.go`.

Two API families, one group each:

| Family | Group | Version | Who publishes it | Pin |
|---|---|---|---|---|
| OPI | `config.openshift.io` | v1 | dpu-operator (fork + unmerged PR #6) | commit `c107ef71` |
| NVIDIA DPF provisioning | `provisioning.dpu.nvidia.com` | v1alpha1 | NVIDIA DOCA Platform | v26.4.0 |
| NVIDIA DPF services | `svc.dpu.nvidia.com` | v1alpha1 | NVIDIA DOCA Platform | v26.4.0 |
| AMD DSC (stubs only) | `dpu.amd.com` | v1alpha1 | not published; envtest stubs | Unpinned |

## 1. OPI — `DpuOperatorConfig`

- CRD: `dpuoperatorconfigs.config.openshift.io`
- Scope: Cluster
- Sample: `config/samples/dpuoperatorconfig-nvidia.yaml`
- Watcher: Lifecycle Manager (`pkg/lifecycle`)

| Path | Type | Constraint | Writer / reader |
|---|---|---|---|
| `spec.logLevel` | integer | original OPI field | human |
| `spec.vendor` | string | enum `nvidia` \| `amd`, maxLength 16, CEL `self == 'nvidia' \|\| self == 'amd'` | human. LCM reads it first |
| `status.conditions[]` | metav1 Condition | no preserve-unknown-fields | LCM writes `VendorOperatorSupported`. Daemon owns `Ready` |

Dual enablement path (annotation), sanitized the same way:

| Annotation | Meaning |
|---|---|
| `lifecycle.opi.nvidia.com/vendor` | fallback if `spec.vendor` is empty |
| `lifecycle.opi.nvidia.com/vendor-operator-version` | installed DPF version, e.g. `v26.4.0` |

`SanitizeVendorToken` (`pkg/lifecycle/version.go`): trim, lowercase,
`[a-z][a-z0-9-]{0,15}`. Path / URL / shell fragment → token `invalid` →
`UnknownVendor`. The raw string is not copied into the condition message.

`VendorOperatorSupported` reasons: `Supported`, `TooOld`, `TooNew`,
`Unknown`, `Unpinned`, `UnknownVendor`.

Live on dh2 after the fix: `spec.vendor=nvidia`, condition
`VendorOperatorSupported=True` reason `Supported`
(`nvidia operator v26.4.0 is within the supported range [v26.4.0, v27.0.0)`).

## 2. OPI — `DataProcessingUnit`

- CRD: `dataprocessingunits.config.openshift.io`
- Scope: Cluster on dh2
- Mapping: `config/mappings/dataprocessingunit.yaml`
- Vendor guard: `spec.dpuProductName` starts with `BlueField`
  (default `BlueField` if unset)

| Path | Role |
|---|---|
| `spec.dpuProductName` | vendor/model. Routes NVIDIA vs AMD |
| `spec.isDpuSide` | unused by the NVIDIA mapping (AMD derives mode from it) |
| `spec.nodeName` | copied to `DPU.spec.dpuNodeName` |
| `status.conditions` | daemon `Ready`; translator `DPFReady` |

Hardware identity is annotation-stamped by the VSP, never invented:

| Annotation | Lands on |
|---|---|
| `provisioning.dpu.nvidia.com/serial-number` | `DPUDevice.spec.serialNumber`, `DPU.spec.serialNumber` (required) |
| `dpu.nvidia.com/bfb-url` | `BFB.spec.url` (required) |
| `dpu.nvidia.com/bfb` | BFB / DPU name, default `bf-bundle` |
| `dpu.nvidia.com/flavor` | Flavor / DPU name, default `dpf-default-flavor` |

Emits (namespace `dpf-operator-system` when the source is cluster-scoped):

| Kind | Name | Mapping `to:` |
|---|---|---|
| DPUDevice | `<name>-device` | `spec.serialNumber` |
| DPUFlavor | annotation or `dpf-default-flavor` | `spec.dpuMode=dpu` |
| BFB | annotation or `bf-bundle` | `spec.url` |
| DPU | source name | `spec.{dpuNodeName,dpuDeviceName,dpuFlavor,bfb,serialNumber,nodeEffect.noEffect}` |

`DPFReady` is True only when every child that publishes readiness is Ready.
DPU/BFB default `status.phase=Initializing` and often have no Ready
condition; empty conditions plus `Initializing` is not treated as ready.

Live on dh2: object `dh2-bluefield-2`, product `BlueField-2`, node `dh2`,
serial `MT2321XZ0KVX`. `DPFReady=False` because DPU and BFB are still
`phase=Initializing`. That is DPF runtime, not a missing field.

## 3. OPI — `ServiceFunctionChain`

- CRD: `servicefunctionchains.config.openshift.io`
- Scope: Namespaced
- Mapping: `config/mappings/servicefunctionchain.yaml`
- Chart fields come from unmerged PR #6. `patchState: unmerged`.
  Do not treat them as upstream-main.

| Path | Role |
|---|---|
| `spec.networkFunctions[].name` | NF name → DPUService name |
| `spec.networkFunctions[].chart.repository` | `DPUService.spec.helmChart.source.repoURL` |
| `spec.networkFunctions[].chart.name` | `DPUService.spec.helmChart.source.chart` |
| `spec.networkFunctions[].chart.version` | `DPUService.spec.helmChart.source.version` |
| `spec.networkFunctions[].image` | skipped (DPF has no bare-image field) |
| CEL `has(self.image) != has(self.chart)` | exactly one of image or chart |
| `status.conditions` | translator writes `DPFReady` only |

Direct ownership. Never emit `DPUDeployment`, `DPUServiceTemplate`, or
`DPUServiceConfiguration` (those controllers would fight for the same
children, and for BFB/Flavor already owned by the DPU mapping).

| Generated CR | Name | Owner |
|---|---|---|
| DPUService | `item.name` | ServiceFunctionChain |
| DPUServiceInterface | `item.name + '-ifc'` | same |
| DPUServiceChain | source name | same |

Same-ns: controller ownerRef + Kubernetes GC.
Cross-ns: annotation + finalizer `translation.opi.nvidia.com/cleanup`.

Live on dh2: `default/sfc-chart-probe` with chart `hbn` /
`https://helm.ngc.nvidia.com/nvidia/doca` / `v25.10.1`.
`status.conditions[DPFReady]=True` reason `AllServicesReady`.

## 4. DPF provisioning — kinds the DPU mapping writes

Group `provisioning.dpu.nvidia.com/v1alpha1`. Fixtures:
`test/e2e/crds/dpf/`.

| Kind | Mapping writes | Live on dh2 (2026-10-23) |
|---|---|---|
| DPUDevice | `spec.serialNumber` (required) | `MT2321XZ0KVX` |
| DPUFlavor | `spec.dpuMode=dpu` | `dpu` |
| BFB | `spec.url` (required) | mellanox `bf-bundle-2.7.0-33` URL |
| DPU | `dpuNodeName`, `dpuDeviceName`, `dpuFlavor`, `bfb`, `serialNumber`, `nodeEffect.noEffect=true` | all present |

Present on the live v26.4 CRD, **not written** by the mapping
(unused extras, not dead writes):

| Field | Why unused |
|---|---|
| `DPU.spec.astraEnabled` | OPI has no equivalent |
| `DPUDevice.spec.cluster` | OPI has no equivalent |
| `DPUDevice.spec.bmcCredentialSecretName` | OPI has no equivalent |

`DPU.spec.bfb` is a string (the BFB CR name), one-of with
`blueFieldSoftware` on the v26.4 schema.

## 5. DPF services — kinds the SFC mapping writes

Group `svc.dpu.nvidia.com/v1alpha1`.

| Kind | Mapping writes | Live on dh2 |
|---|---|---|
| DPUService | `spec.serviceID`, `helmChart.source.{repoURL,chart,version}`, `interfaces`, `paused=false`, `security.privileged=true` | `default/hbn` — all set, including `privileged=true` |
| DPUServiceInterface | nested `template.spec.template.spec.{interfaceType=service, service.{serviceID,interfaceName=net0,network=default}}` plus label `service=<nf>` | `default/hbn-ifc` |
| DPUServiceChain | `spec.template.spec.template.spec.switches` — one switch, one port per chart NF, `matchLabels.service=<nf>` | `default/sfc-chart-probe` |

`security.privileged` is required by v26.4 CEL when `deployInCluster` is
false (the default). The Sept 7 live CRD did not have `spec.security`.
That was the first dead write. After the authorized v26.4 CRD replace,
dry-run and the live `hbn` object both keep the field.

Not generated (DPF-owned descendants — do not SSA):

`ServiceInterfaceSet`, `ServiceInterface`, `ServiceChainSet`,
`ServiceChain`, Helm releases / DaemonSets.

## 6. Status writers (single writer per GVK + type)

Table: `config/condition-writers.yaml`.

| Object | Type | Writer | Schema |
|---|---|---|---|
| DataProcessingUnit | `Ready` | vsp-opi-daemon | conditions |
| DataProcessingUnit | `DPFReady` | mapping:dataprocessingunit | conditions |
| DataProcessingUnit | `DSCReady` | mapping:amd-dsc200 | conditions |
| ServiceFunctionChain | `DPFReady` | mapping:servicefunctionchain | conditions |
| DpuOperatorConfig | `Ready` | vsp-opi-daemon | conditions |
| DpuOperatorConfig | `VendorOperatorSupported` | lifecycle | conditions |

`Ready` is reserved. A mapping that writes it fails `Spec.Validate`.
Two writers of the same type fail the writer-table tests.

The translator also reads the **live** CRD OpenAPI and omits undeclared
`spec` fields, then retries if apply still reports `unknown field`
(`pkg/translation/dead_fields.go`). Mappings stay on the fixtures.

## 7. AMD — `dpu.amd.com` (stubs, not a live API on dh2)

Mapping `config/mappings/amd-dsc200.yaml` is data-only. Target kinds:
`DSCDevice`, `DSCProfile`, `DSCFirmware`, `DSCNodePolicy`.

Guards

- Product must match `DSC|Pensando` (the dh1 VPD string starts with
  `Pensando`, not `DSC`).
- Serial annotation `dpu.amd.com/serial-number` must be non-empty.
  Empty serial → no emit, no `DSCReady` (no fake device).

Stub CRD hardening (envtest only)

- `DSCDevice.spec.deviceSerial`: minLength 1, maxLength 64, pattern
  `^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`
- `pciAddress`: `^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$`
- `DSCFirmware.spec.image.url`: `^https://` only
- management driver enum `ionic`; interface enum `oob` \| `inband`

On dh2 the stub CRDs were deleted. The live mappings ConfigMap does not
load `amd-dsc200.yaml`. Stubs stay under `test/conformance/crds/amd`
for envtest.

## 8. Live vs fixture (after the 2026-10-23 aligns)

| Field | Fixture | Live dh2 | Mapping writes it? | Result |
|---|---|---|---|---|
| `DpuOperatorConfig.spec.vendor` | yes, enum | yes, enum | no (human / LCM) | LIVE |
| SFC `status.conditions` | yes | yes | `DPFReady` | LIVE |
| `DPUService.spec.security.privileged` | yes | yes (replaced) | yes | LIVE |
| `DPU.spec.astraEnabled` | yes | yes (replaced) | no | unused |
| `DPUDevice.spec.cluster` | yes | yes (replaced) | no | unused |
| AMD DSC* CRDs | stubs in-repo | absent | not loaded | correct |

## 9. What is not an API failure

DPU and BFB `status.phase=Initializing` with empty `status.conditions`.
Every mapped `spec` field is on the CRD and on the object. The DPF
operator has not finished provisioning the BlueField. The adapter has
nothing left to write.
