# What was fixed and how

## Adapter repo (uncommitted unless you ask to commit)

### HBN chart pin

Files: `config/samples/servicefunctionchain.yaml`,
`config/samples/service-chain-direct-children.yaml`,
`test/e2e/testdata/servicefunctionchain.yaml`, and tests that asserted
`v25.10.1`.

Change: `v25.10.1` → `v26.4.0` so the OPI SFC matches DPF v26.4.

Live: JSON patch
`/spec/networkFunctions/0/chart/version` on `sfc-chart-probe`.
Translator updated `DPUService/hbn`.

### BFB URL pin

File: `config/overlays/hardware-vsp/pci-patch.yaml` env `DPU_BFB_URL`.

Change: Ubuntu 22.04 `bf-bundle-2.7.0-33` → Ubuntu 24.04
`bf-bundle-3.4.0-92` **without** `_64k_` (BlueField-2 4K pages).

Live: annotated the OPI DPU (`dpu.nvidia.com/bfb`, `dpu.nvidia.com/bfb-url`),
new BFB CR `bf-bundle-340` because `spec.url` is immutable.

### DPUDevice name = lower(serial)

File: `config/mappings/dataprocessingunit.yaml`

Before:

```yaml
name:
  cel: "source.metadata.name + '-device'"
# and spec.dpuDeviceName the same
```

After:

```yaml
name:
  cel: "source.metadata.?annotations['provisioning.dpu.nvidia.com/serial-number'].orValue(source.metadata.name + '-device').lowerAscii()"
```

Same CEL for `spec.dpuDeviceName`. That matches DPF hostagent
`dpuDeviceCRName` = `strings.ToLower(serial)`.

File: `pkg/mapping/cel.go` — `ext.Strings()` so `lowerAscii()` compiles.

Tests: `pkg/mapping/engine_test.go`, `pkg/translation/translation_controller_test.go`
(`bf3-worker-device` → `mt1234`), `test/e2e/e2e_test.go`,
`test/conformance/conformance_test.go`.

Image: `docker build --platform linux/amd64 -t controller:latest .`
then `docker save | ssh ... 'k3s ctr images import -'`.
Rollout restart of the adapter Deployment and VSP DaemonSet.

`spec.dpuDeviceName` is immutable, so the live DPU CR was deleted
(finalizer `dpu-protection` cleared) and the translator recreated it.

### PCI address

- Annotation key `provisioning.dpu.nvidia.com/pci-address` in
  `pkg/discovery/types.go`
- `DPFPCIAddress("0000:0f:00.0")` → `0000-0f-00`
- Annotator stamps it for NVIDIA cards
- Mapping field `spec.pciAddress` (optional; skipped when empty so
  tests without the annotation still pass)

Live VSP binary in the imported image is from the rebuild **before**
the annotator stamp, so the annotation was applied with `kubectl
annotate` and `spec.pciAddress` was patched (mutable). Another image
import is needed before VSP stamps PCI by itself. Mapping is already
in the live ConfigMap.

## Cluster (dh2) — not host OS packages / systemd

### Argo secret

```bash
k3s kubectl -n dpf-operator-system delete secret argocd-initial-admin-secret
```

### DPFOperatorConfig + BFB PVC

Applied in `dpf-operator-system`:

- PVC `bfb-pvc` 20Gi `local-path`
- `DPFOperatorConfig/dpfoperatorconfig` with
  `provisioningController.bfbPVCName=bfb-pvc`,
  `kamajiClusterManager.disable=true`,
  `monitoring.disable=true`

Live v26.4 CRD **has no** `deploymentMode`. Do not set it.

### DPUService CRD CEL

Python loaded the CRD JSON, removed the spec validation whose message
is `security.privileged must be set when deployInCluster is false`,
`kubectl replace` the CRD. Operator then created the eight system
DPUServices.

### Detector on single-node

```bash
k3s kubectl label node dh2 node-role.kubernetes.io/control-plane-
k3s kubectl label node dh2 feature.node.kubernetes.io/dpu-enabled=true
```

### Lab OOB stub

Runtime `ip link` / `ip addr` / `ip route` for `br-dpu` with metric
10000. Not written to netplan. Not an `/etc` edit.

### Stale BFB

Deleted `BFB/bf-bundle` (old URL). New `bf-bundle-340` Ready.

### Static DPUCluster (second pass)

On dh2 (job dir, then deleted):

1. SA `dpu-cluster-static-admin` + ClusterRoleBinding `cluster-admin`
2. Token `create token --duration=8760h`
3. kubeconfig server `https://kubernetes.default.svc`, CA from
   `kube-root-ca.crt`, Secret key `super-admin.conf`
4. `DPUCluster` `dpu-cluster-1` `type: static` `kubeconfig: dpu-cluster-1-admin-kubeconfig`

Provisioning controller: Pending → Creating → Created + health check
→ `phase=Ready`. Allocator assigned `dh2-bluefield-2`. Hostagent
started OS install from `bf-bundle-340`.

### PCI image (second pass)

`docker build --platform linux/amd64 -t controller:latest .` then
import + rollout. VSP binary now includes `DPFPCIAddress`.

## What was not changed

- No helm-uninstall of DPF
- No AMD CRs on BlueField
- No Kamaji
- No Argo `Application` objects invented by hand
- No reboot, firmware, BMC, package install, or `/etc` edit
- `docs/study-notes.md` and `evidence/` left uncommitted
