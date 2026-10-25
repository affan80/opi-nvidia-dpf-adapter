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

### DPF mapping fields vs the CRDs still installed on dh2

The operator **image** is v26.4.0. The provisioning CRDs that already
existed on 2026-09-07 were labelled by Helm `--take-ownership` and were
**not** replaced. Those live schemas are older than the fixtures:

| Field the mapping writes | v26.4 fixture | live CRD on dh2 |
|---|---|---|
| `DPUService.spec.security.privileged` | yes | **missing** (strict `unknown field`; translator drops it and retries) |
| `DPU.spec.bfb` | one-of with `blueFieldSoftware` | listed in `required` |
| `DPU.spec.astraEnabled` | yes | no (mapping does not write it) |
| `DPUDevice.spec.cluster` / `bmcCredentialSecretName` | yes | no (mapping does not write them) |

The fields already used for the BlueField object on dh2
(`serialNumber`, `dpuNodeName`, `bfb`, `nodeEffect.noEffect`) exist on
both schemas. The DPU staying `Initializing` is not a field-name error.

Do not upgrade those CRDs from this repo. This adapter does not own them,
and the lab servers are not to be changed for a schema bump.

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
#    SFC fixtures still carry unmerged PR #6 chart fields,
#    versions.yaml agrees with go.mod and the NVIDIA window.
make verify-api

# Same tests, named (what make verify-api runs):
go test ./pkg/mapping/ ./pkg/lifecycle/ -count=1 -run \
  'TestMappingFieldsExistInVendoredCRDs|TestSFCFixturesMatchPinnedHelmChartPatch|TestPinnedVersionsAgree|TestPinnedNVIDIAWindowMatchesVersionsYAML'

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
