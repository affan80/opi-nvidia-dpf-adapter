# API / schema agreement

Pin versions only after comparing:

1. the OPI CRDs installed on the lab with `test/e2e/crds/opi`
2. the DPF fields the mappings write with the target CRDs
3. those target CRDs with `test/e2e/crds/dpf`

`config/versions.yaml` is the pin. `pkg/lifecycle/version.go` is the runtime
window published as `VendorOperatorSupported`. They must match.
`go test ./pkg/mapping/ -run TestMappingFieldsExistInVendoredCRDs` is the
drift gate.

## Pinned (2026-10-22)

| API | Version | Evidence |
|---|---|---|
| Kubernetes | **v1.36.4** (k3s `v1.36.4+k3s1`) | live `k3s --version` on lab host dh2 |
| client-go / apimachinery | **v0.36.0** | `go.mod` |
| OPI CRDs | `config.openshift.io/v1` @ `c107ef719f74d4f841ceb1ed45bbd293b3739a3c` (**unmerged** [opiproject/dpu-operator#6](https://github.com/opiproject/dpu-operator/pull/6), fork `feat/sfc-helm-chart`) | HelmChartSource is not on upstream main. Fixtures and the SFC mapping pin that patch. |
| NVIDIA DPF | **v26.4.0** | operator image `nvcr.io/nvidia/doca/dpf-system:v26.4.0`, Helm chart `dpf-operator-v26.4.0` |
| DPF support window | **[v26.4.0, v27.0.0)** | mappings were compared to the v26.4 fixtures, not to v25 |

AMD has no published operator release. It stays `Unpinned`.

## What the comparison found

### OPI fixtures vs installed CRDs

DataProcessingUnit, ServiceFunctionChain, and DpuOperatorConfig match at
the property-path level. Both sides are `v1`. SFC has
`spec.networkFunctions[].chart.{name,repository,version}` and the
image-versus-chart validation. SFC `status` is still an empty object, so
the `DPFReady` mirror is pruned until upstream adds `conditions`.

`DpuOperatorConfig.spec` still has only `logLevel`. There is no
`spec.vendor`. The lifecycle manager keeps reading the annotation.

### DPF mapping fields vs v26.4 fixtures

Every `to:` path in `dataprocessingunit.yaml` and
`servicefunctionchain.yaml` exists on the vendored CRDs:

| Mapping writes | CRD |
|---|---|
| `DPUDevice.spec.serialNumber` | required |
| `DPUFlavor.spec.dpuMode` | present |
| `DPUFlavor.spec.nvconfig` | official 26.4.0 HBN (`PF_TOTAL_SF=20`, `PF_SF_BAR_SIZE=10`); flavor spec immutable after create |
| `BFB.spec.url` | required |
| `DPU.spec.{dpuNodeName,dpuDeviceName,dpuFlavor,bfb,serialNumber,nodeEffect.noEffect}` | required set + `bfb`/`blueFieldSoftware` one-of |
| `DPUService.spec.{serviceID,helmChart.source.repoURL,helmChart.source.version,interfaces,paused,security.privileged}` | `helmChart` required; `security.privileged` required by CEL when `deployInCluster` is false |
| `DPUServiceInterface.spec.template.spec.template.spec.{interfaceType,service.*}` | `template` required; `service` required when `interfaceType=service` |
| `DPUServiceChain.spec.template.spec.template.spec.switches` | `template` required; `switches` minItems 1 |

### DPF mapping fields vs the CRDs installed on dh2

Rechecked 2026-10-25 against the storage schemas of the CRDs created
2026-09-07. Those objects still have that creation time, and their
OpenAPI now matches the v26.4.0 fixtures for every field this mapping
writes. `DPU.spec.bfb` is not a plain required string: the live CEL is
`exactly one of bfb or blueFieldSoftware must be set`, same as the
fixture. `DPU.spec.pciAddress`, `DPU.spec.astraEnabled`,
`DPUDevice.spec.cluster`, `DPUDevice.spec.bmcCredentialSecretName`, and
`DPUService.spec.security.privileged` are present. The mapping does not
write `astraEnabled`, `cluster`, or the BMC secret.

A supported NVIDIA source (`dpuProductName` starting with `BlueField`,
serial, PCI `0000-0f-00`, and an http(s) BFB URL) emits four namespaced
objects in `dpf-operator-system`, `provisioning.dpu.nvidia.com/v1alpha1`:

| Kind | Required by the CRD | What the mapping sets |
|---|---|---|
| `DPUDevice` | `spec.serialNumber` (minLength 1) | lower-cased serial as the name and `spec.serialNumber` |
| `DPUFlavor` | none; `dpuMode` enum `dpu\|zero-trust\|nic`; nvconfig parameter `KEY=VALUE`, device `*` must be the only entry | `dpuMode=dpu`, one `nvconfig` entry `device=*` |
| `BFB` | `spec.url` matching `^(http\|https)://.+$` | annotation `dpu.nvidia.com/bfb-url` |
| `DPU` | `dpuDeviceName`, `dpuFlavor`, `dpuNodeName`, `nodeEffect`, `serialNumber`; exactly one of `bfb` / `blueFieldSoftware`; `pciAddress` pattern `0000-0f-00` | all five required fields, `spec.bfb` only, `nodeEffect.noEffect=true`, PCI annotation |

`go test ./pkg/mapping/ -run TestNVIDIASourceEmitsSchemaValidDPFResources`
runs that emit through the pinned CRD OpenAPI validators. A non-BlueField
product emits nothing. Do not upgrade these CRDs from this repo.

### Mapping drop found against the OPI source schema

OPI requires `chart.name`. DPF has `spec.helmChart.source.chart`. The
mapping copied `repository` and `version` and dropped the chart name.
That is now written through.

## How to re-run the comparison

Run these from `opi-nvidia-dpf-adapter/` **before** changing mappings or pins.
Do not start irreversible work (Helm DPF install, CRD upgrades, DPUDeployment)
if any of them fail.

```bash
# 1. Official gate: mapping to: paths exist on vendored CRDs,
#    and a BlueField source emits a schema-valid DPUDevice/DPUFlavor/BFB/DPU.
#    SFC fixtures still carry unmerged PR #6 chart fields,
#    versions.yaml agrees with go.mod and the NVIDIA window.
make verify-api

# Same tests, named (what make verify-api runs):
go test ./pkg/mapping/ ./pkg/lifecycle/ -count=1 -run \
  'TestMappingFieldsExistInVendoredCRDs|TestNVIDIASourceEmitsSchemaValidDPFResources|TestNonBlueFieldSourceEmitsNoDPFResources|TestSFCFixturesMatchPinnedHelmChartPatch|TestPinnedVersionsAgree|TestPinnedNVIDIAWindowMatchesVersionsYAML'

# 2. FieldMapping JSON Schema vs Go structs + shipped YAML $schema
go test ./pkg/mapping/ -count=1 -run \
  'TestSchemaMatchesGoStructs|TestSchemaReservesReadyCondition|TestShippedMappingsDeclareTheSchema'

# 3. Writer table vs CRD status.conditions (Ready reserved, SFC pruned)
go test ./pkg/mapping/ -count=1 -run \
  'TestWriterTableSchemaAgreesWithVendoredCRDs|TestShippedMappingsMatchWriterTable|TestValidateRequiresStatusSchema'

# 4. Direct SFC graph + one owner per DPF kind
go test ./pkg/mapping/ -count=1 -run \
  'TestSFCMappingIsDirectOwnership|TestNoTwoMappingsOwnTheSameDPFKind'

# 5. Design contract still records the pins
go test ./pkg/mapping/ -count=1 -run TestHybridDesignDocIsTheContract

# 6. Runtime window (v26.4.0 Supported, v25 TooOld, v27 TooNew)
go test ./pkg/lifecycle/ -count=1 -run \
  'TestPinnedNVIDIAWindowMatchesVersionsYAML|TestSupportedRangeIsWellFormed|TestClassifySkew|TestKnownVendor'

# 7. Full packages that consume the schemas
go test ./pkg/mapping/ ./pkg/lifecycle/ ./test/conformance/ -count=1

# 8. ClusterRole verbs match mapping emit kinds
make verify-rbac
```

Against a live cluster (read-only; do not upgrade CRDs from this repo):

```bash
# On dh2, after F5 VPN is up
k3s kubectl get crd dataprocessingunits.config.openshift.io -o yaml
k3s kubectl get crd servicefunctionchains.config.openshift.io -o yaml
k3s kubectl get crd dpuoperatorconfigs.config.openshift.io -o yaml
k3s kubectl get crd dpus.provisioning.dpu.nvidia.com -o yaml
k3s kubectl get crd dpuservices.svc.dpu.nvidia.com -o yaml
k3s --version
```

Known live skew (2026-10-22): `DPUService.spec.security` is on the v26.4
fixture and **missing** on dh2. Mappings stay aligned to the fixtures.

Re-pin only after both fixture tests and a live read still agree.
Command log from the 2026-10-23 re-run:
`~/opi_test_report/api_schema_agreement/testing_commands.md`.
