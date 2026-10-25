# What we fixed, and how

Each item is: what broke, root cause, the change, how we knew it was fixed.
Code paths are under `opi-nvidia-dpf-adapter/`.

---

## 1. DPF support window said v25, lab runs v26.4.0

What broke
- LCM window was `[v25.1.0, v26.0.0)`.
- dh2 image is `nvcr.io/nvidia/doca/dpf-system:v26.4.0`.
- That install would have been `TooNew`.

Root cause
- The window was an aspiration, not a comparison against the fixtures
  and the running operator.

Fix
- `pkg/lifecycle/version.go` `supportedVersions["nvidia"]` =
  `{MinInclusive: "v26.4.0", MaxExclusive: "v27.0.0"}`
- Same window in `config/versions.yaml`
- Tests updated so v26.4.0 is Supported and v25.4.0 is TooOld

How we verified
- `ClassifySkew(nvidia, v26.4.0) = Supported`
- Live: `VendorOperatorSupported=True` reason `Supported`

---

## 2. No pin file, fixtures and go.mod could drift

What broke
- Versions lived in comments, tests, and docs separately.

Fix
- `config/versions.yaml` is the pin (K8s, OPI commit + PR #6, DPF window).
- `TestPinnedVersionsAgree`, `TestPinnedNVIDIAWindowMatchesVersionsYAML`,
  `TestHybridDesignDocIsTheContract` fail if they disagree.
- Design contract: `docs/hybrid-integration-design.md`

How we verified
- `make verify-api` PASS

---

## 3. SFC mapping dropped Helm chart name

What broke
- OPI requires `networkFunctions[].chart.name`.
- Mapping copied `repository` and `version` only.
- Helm would get a URL and a version with no chart identity.

Fix
- `config/mappings/servicefunctionchain.yaml`:

```yaml
- to: spec.helmChart.source.chart
  cel: "item.chart.name"
  required: true
```

How we verified
- Live `DPUService/hbn`: `spec.helmChart.source.chart=hbn`
- `TestMappingFieldsExistInVendoredCRDs` PASS

---

## 4. Chart fields treated as if they were on upstream main

What broke
- `spec.networkFunctions[].chart` is only on unmerged
  [opiproject/dpu-operator#6](https://github.com/opiproject/dpu-operator/pull/6).
- A silent upgrade to main would drop chart and stop emitting children.

Fix
- Pin `opi.patchState: unmerged`, `opi.pullRequest` #6, commit `c107ef71`.
- Fixtures under `test/e2e/crds/opi` include chart + CEL
  `has(self.image) != has(self.chart)`.
- `TestSFCFixturesMatchPinnedHelmChartPatch` fails if those go missing.
- Mapping `when: "'chart' in item"` so image-only NFs emit nothing.

How we verified
- Live SFC `sfc-chart-probe` admits a chart NF.
- Image-only NF creates no DPUService.

---

## 5. CEL `ref.Val` wrappers blocked Server-Side Apply

What broke
- Chain switches and interface labels are CEL lists/maps.
- cel-go returned `[]ref.Val` / `map[ref.Val]ref.Val`.
- SSA: `json: unsupported type: map[ref.Val]ref.Val`.

Fix
- `pkg/mapping/cel.go` `celNative` recursively unwraps to `[]any` /
  `map[string]any` / string / bool / int64 / float64.

How we verified
- `TestSFCApplyEmitsOwnedGraph`: label is the Go string `hbn`, one switch.
- Live `hbn-ifc` label `service=hbn`; chain ports `matchLabels.service=hbn`.

---

## 6. Direct SFC ownership (never DPUDeployment)

What broke
- Emitting `DPUDeployment` would give DPF's controller the same
  DPUService / interface / chain, and DPUSets against BFB/Flavor the
  DPU mapping already owns.

Fix
- Mapping emits only `DPUService`, `DPUServiceInterface`, `DPUServiceChain`.
- `TestSFCMappingIsDirectOwnership` / `TestNoTwoMappingsOwnTheSameDPFKind`.

How we verified
- Those tests PASS. Live children of `sfc-chart-probe` are exactly those
  three kinds.

---

## 7. Dead write: `DPUService.spec.security.privileged`

What broke
- v26.4 CEL requires `security.privileged` when `deployInCluster` is false.
- Live CRD (created 2026-09-07, Helm `resource-policy: keep` after
  `--take-ownership`) had no `spec.security`.
- SSA: `.spec.security: field not declared in schema`.
- `sfc-chart-probe` had **zero** children.

Root cause
- Operator **image** was v26.4.0. Provisioning/svc CRDs were still the
  Sept 7 schemas. Interface CRD was newer (2026-10-20).

Fix (both, as authorized)
1. Translator omit: `pkg/translation/dead_fields.go` reads live CRD
   OpenAPI, drops undeclared spec fields, retries on `unknown field`.
2. Replaced live CRDs for DPUService, DPU, DPUDevice with the v26.4
   fixtures. Restored Helm keep annotations. Mapping stayed on the
   fixtures (Kind/conformance still need the CEL field).

Also
- ClusterRole was missing `dpuserviceinterfaces` / `dpuservicechains`
  (Forbidden list). Applied updated `config/rbac/role.yaml` as
  `opi-nvidia-dpf-adapter-manager-role`.
- Stale mappings ConfigMap crashed the new binary (`status.schema`
  required). Refreshed the ConfigMap.

How we verified
- Dry-run with `spec.security.privileged: true` → created (server dry run).
- Live `DPUService/hbn` has `spec.security.privileged=true`.
- Children exist: `hbn`, `hbn-ifc`, `sfc-chart-probe`.

---

## 8. `DpuOperatorConfig.spec.vendor` was a dead field

What broke
- OPI CRD spec had only `logLevel`.
- Dry-run: unknown field `spec.vendor`.
- Enablement was annotation-only. A free-form annotation is a future
  Helm/OLM injection hole if the LCM ever execs it.

Fix
- Pinned fixture + live CRD: `spec.vendor` enum `nvidia|amd`, maxLength 16,
  CEL. No preserve-unknown-fields.
- Annotation kept as fallback.
- `SanitizeVendorToken` on both paths.
- Sample sets both `spec.vendor: nvidia` and the annotation.
- Conformance now uses pinned fixtures (not a sibling dpu-operator
  checkout that still lacks the field).

How we verified
- Live patch: `spec.vendor=nvidia` accepted.
- Dry-run `acme` / `../../../etc/passwd` / `nvidia;id` rejected by enum.
- Annotation `acme` → `VendorOperatorSupported=False` reason `UnknownVendor`.
- Unit: `TestSanitizeVendorTokenRejectsInjection`.

---

## 9. SFC `status.conditions` was pruned

What broke
- OPI SFC `status` was `{type: object}` with no properties.
- Apiserver pruned any write. Mapping correctly used `status.schema: pruned`
  so we did not silently drop `DPFReady`.

Fix
- Fixture + live CRD: standard metav1 `status.conditions` (required
  lastTransitionTime / message / reason / status / type). No
  preserve-unknown-fields.
- Mapping and `config/condition-writers.yaml` flipped to `schema: conditions`.
- Controller writes `DPFReady` only. Daemon still owns `Ready`.

How we verified
- Live `sfc-chart-probe`:
  `status.conditions[DPFReady]=True` reason `AllServicesReady`.
- `TestWriterTableSchemaAgreesWithVendoredCRDs` PASS.

---

## 10. AMD stub CRDs on a BlueField host (fake-device API)

What broke
- Stub CRDs `dscdevices/dscprofiles/dscfirmwares/dscnodepolicies.dpu.amd.com`
  were installed on dh2 2026-10-20. No Pensando card. `deviceSerial`
  was required but `""` satisfied required (no minLength).

Fix
- Mapping `when` requires a non-empty `dpu.amd.com/serial-number` on
  every emit **and** on status (otherwise `DSCReady` would land on a
  card with no serial).
- Stub CRD: serial minLength + pattern; firmware URL `https://` only;
  PCI pattern; driver/interface enums.
- Deleted the four stub CRDs from dh2.
- Live ConfigMap is NVIDIA-only (`dataprocessingunit.yaml`,
  `servicefunctionchain.yaml`). Loading `amd-dsc200.yaml` made
  controller-runtime ERROR on missing AMD kinds. Stubs stay in
  `test/conformance/crds/amd` for envtest.

How we verified
- `k3s kubectl get crd | grep -E 'amd|dsc'` → none.
- `TestAMDMappingRequiresSerial`: Pensando product, no serial → 0 objects,
  no status.
- After dropping the mapping from the live ConfigMap: no AMD watch errors.

---

## 11. Enablement / condition collisions (earlier contract)

What broke
- Reading only `spec.vendor` on a CRD that pruned it made LCM a no-op
  (no skew condition ever).
- Writing `Ready` would fight the daemon and the printer column.

Fix
- Annotation fallback + spec precedence.
- LCM writes only `VendorOperatorSupported`.
- Mappings write only `DPFReady` / `DSCReady`.
- Writer table is the lock.

How we verified
- Live config has `VendorOperatorSupported`, not a translator `Ready`.
- OPI DPU has `DPFReady`, not a translator `Ready`.

---

## What we did **not** treat as a dead API

| Symptom | Why it is not a schema bug |
|---|---|
| DPU / BFB `phase=Initializing` | DPF operator has not finished. All mapped spec fields are present |
| DPUService / interface / chain READY column empty | DPF has not published Ready conditions. SFC `DPFReady` treats empty child conditions as non-blocking |
| `DPU.spec.astraEnabled` unused | Mapping does not write it |
| No AMD objects on dh2 | Correct. No Pensando card; stubs removed |

---

## Deploy steps used on dh2 (after a Go or mapping change)

Job dir only. Deleted afterward.

```bash
JOB=/tmp/lab-job-<name>
ssh "${SSH_OPTS[@]}" root@172.22.1.2 "rm -rf $JOB && mkdir -p $JOB"
rsync -az --delete -e "ssh ${SSH_OPTS[*]}" \
  --exclude '.git' --exclude 'bin' --exclude 'cover.out' --exclude 'evidence' \
  /Users/kartikey0104/Desktop/opi-dpf-adapter/opi-nvidia-dpf-adapter/ \
  "root@172.22.1.2:$JOB/"

# mappings (NVIDIA only on this host)
ssh ... "k3s kubectl -n opi-nvidia-dpf-adapter-system create configmap \
  opi-nvidia-dpf-adapter-mappings --from-file=$JOB/config/mappings/dataprocessingunit.yaml \
  --from-file=$JOB/config/mappings/servicefunctionchain.yaml \
  --dry-run=client -o yaml | k3s kubectl apply -f -"

# image
ssh ... "cd $JOB && docker build --platform linux/amd64 -t controller:latest . \
  && docker save controller:latest | k3s ctr images import - \
  && k3s kubectl -n opi-nvidia-dpf-adapter-system delete pod -l control-plane=controller-manager \
  && rm -rf $JOB"
```

Do not copy `opi_lab_key`. Do not leave `/tmp/lab-job-*`.
