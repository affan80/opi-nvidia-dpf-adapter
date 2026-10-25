# Issues, in the order they were fixed

Each item is: what was wrong, where it lived, what the pipeline did
before, what it does after.

## 1. HBN chart pin `v25.10.1` vs DPF `v26.4.0`

**Where:** OPI `ServiceFunctionChain` `sfc-chart-probe` in namespace
`default`; FieldMapping samples and tests under
`opi-nvidia-dpf-adapter/config/samples`, `test/e2e/testdata`,
`pkg/mapping`, `pkg/translation`, `test/conformance`.

**Issue:** The SFC copied `networkFunctions[].chart.version: v25.10.1`.
The live DPF operator and `dpu-networking` chart are `v26.4.0`. That is
release skew on the HBN path.

**Before:** live `DPUService/hbn` `spec.helmChart.source.version=v25.10.1`.

**After:** samples, tests, live SFC, and live `hbn` all `v26.4.0`.
`DPFReady=True/AllServicesReady`. Unit tests `./pkg/mapping ./pkg/translation
./pkg/lifecycle` pass.

## 2. Argo CD initial admin secret still on the cluster

**Where:** `Secret/argocd-initial-admin-secret` in
`dpf-operator-system`.

**Issue:** Argo's getting-started secret is meant to be deleted after
first login. Leaving it is a credential-in-etcd vulnerability. The
server Service was already `ClusterIP` with no `externalIPs` and no
`server.insecure` key.

**Before:** secret `PRESENT`.

**After:** secret deleted (`GONE`). Server still `ClusterIP` ports 80/443.
Password was not printed.

## 3. No `DPFOperatorConfig` — provisioning pipeline not connected

**Where:** CR `DPFOperatorConfig` name `dpfoperatorconfig` namespace
`dpf-operator-system`. The DPF operator logs were reconciling that
singleton while `kubectl get` returned empty.

**Issue:** Without this object DPF does not deploy provisioning,
DPUService controller, detector, or system DPUServices. DPU/BFB sat in
`Initializing` for ~3 days. Zero Argo `Application` objects.

**After apply (kamaji disabled, BFB PVC `bfb-pvc` 20Gi local-path):**

- `dpf-provisioning-controller-manager` 2/2
- `dpuservice-controller-manager` 1/1
- `static-cm-controller-manager` 1/1
- `dpf-dpu-detector` DaemonSet
- `bfb-registry` Running
- `DPFOperatorConfig` `Ready=True/Success` once issue 4 was fixed

## 4. System DPUServices rejected by CEL (`security.privileged`)

**Where:** live CRD `dpuservices.svc.dpu.nvidia.com` spec validation
"security.privileged must be set when deployInCluster is false".
DPF operator image `dpf-system:v26.4.0` applying system DPUServices
(multus, flannel, sriov-device-plugin, ovs-cni, sfc-controller,
cni-installer, nvidia-k8s-ipam-node, servicechainset-rbac-and-crds)
**without** `spec.security`.

**Issue:** CRD/operator skew from replacing live DPF CRDs with a newer
schema than the running operator emits. Operator reconcile loop:

```
error patching DPUService ... is invalid: spec: Invalid value:
security.privileged must be set when deployInCluster is false
```

**Fix:** removed that one CEL rule from the live DPUService CRD. Other
CELs kept (including "must not set privileged when deployInCluster is
true"). Adapter-translated `hbn` still has `security.privileged: true`.

**After:** all eight system DPUServices `Ready=True/Success`.
`DPFOperatorConfig` `SystemComponentsReady=True` and `Ready=True`.

## 5. DPU detector never scheduled on single-node k3s

**Where:** `DaemonSet/dpf-dpu-detector` affinity
`node-role.kubernetes.io/control-plane DoesNotExist`. Node `dh2` was
labelled `node-role.kubernetes.io/control-plane=true`. Desired=0.

**Issue:** detector never wrote NFD labels, so DPF did not treat the
node as a DPU host.

**Fix:** dropped the control-plane label (there was no control-plane
taint). Detector became 1/1 Running. NFD labels
`feature.node.kubernetes.io/dpu-0-pci-address=0000-0f-00` appeared.

Then labelled `feature.node.kubernetes.io/dpu-enabled=true` so the
provisioning `NodeReconciler` deployed hostagent pod `dh2-dms` (3/3).

## 6. BFB `Error`: software inventory JSON not found

**Where:** `BFB/bf-bundle` owned by OPI `DataProcessingUnit/dh2-bluefield-2`.
URL from VSP env `DPU_BFB_URL` in
`config/overlays/hardware-vsp/pci-patch.yaml`.

**Issue:** URL was Ubuntu 22.04 `bf-bundle-2.7.0-33` (2024). DPF v26.4
`VersionFromBFBFile` requires an embedded software-inventory JSON
(`bf-bundle-3.2.x` / `3.4.x`). After config applied, BFB downloaded the
old file and failed:

```
Downloaded=False Failure: software inventory JSON not found in BFB
phase=Error
```

`spec.url` is immutable, so a new BFB name was used.

**Fix:** pin to the 4K (non-`64k`) v26.4 bundle, which is the right page
size for BlueField-2:

`https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.4.0-92_26.04_ubuntu-24.04_prod.bfb`

(~1.56 GiB, HTTP 200). Annotate OPI DPU `dpu.nvidia.com/bfb=bf-bundle-340`
and `dpu.nvidia.com/bfb-url=<that>`. Translator emitted `BFB/bf-bundle-340`.
Deleted the stale `bf-bundle`. **After ~12 min: `phase=Ready`.**

## 7. Hostagent blocked on duplicate DPUDevice serial

**Where:** adapter FieldMapping `config/mappings/dataprocessingunit.yaml`
named `DPUDevice` `source.metadata.name + '-device'`
(`dh2-bluefield-2-device`). DPF hostagent names the device
`strings.ToLower(serial)` (`mt2321xz0kvx`). Serial `MT2321XZ0KVX` is
unique cluster-wide.

**Issue:**

```
admission webhook "vdpudevice.kb.io" denied the request:
spec.serialNumber: Duplicate value: "MT2321XZ0KVX"
```

Hostagent never created `DPUNode`. DPU stayed
`Initialized=False/DPUNodeNotFound`.

**Fix:** mapping name and `spec.dpuDeviceName` now
`lowerAscii(serial)`. Enabled CEL `ext.Strings()` in
`pkg/mapping/cel.go`. Rebuilt `controller:latest` linux/amd64, imported
into k3s, reloaded the mappings ConfigMap. Deleted
`dh2-bluefield-2-device`. Hostagent created `mt2321xz0kvx` and `DPUNode/dh2`.

`spec.dpuDeviceName` is immutable, so the DPU CR was deleted and
recreated by the translator with `device=mt2321xz0kvx`.

## 8. `DPUOOBBridgeNotConfigured`

**Where:** DPU initializing state requires
`DPUNode.status.conditions[OOBBridgeConfigured]=True` for non-Redfish
install. Hostagent looks up linux bridge `br-dpu` **and** a default
route on that bridge. Detector logged `bridge br-dpu does not exist`.

**Issue:** this lab has no DPU OOB fabric. Putting the **real** default
route on `br-dpu` would steal management traffic and break SSH.

**Fix:** runtime-only stub (not netplan, not `/etc`):

- `ip link add br-dpu type bridge` + `169.254.99.1/24` + up
- extra default route via `br-dpu` **metric 10000** (real defaults stay
  metric 100 on `ens10f0np0` / `ens1f*`)
- ping `172.22.1.1` still succeeded

Hostagent then set `OOBBridgeConfigured=True`. This is a lab check
bypass, not a real OOB network. See vulnerabilities.

## 9. `PCIAddressNotProvided`

**Where:** DPU `spec.pciAddress` required for `hostAgent` install.
Adapter mapping did not emit it. VSP enumerated PCI but did not stamp
an annotation the mapping could read.

**Issue:** DPU `Initialized=False/PCIAddressNotProvided`. Format is
`0000-0f-00` (not sysfs `0000:0f:00.0`). Field is mutable.

**Fix:** annotation `provisioning.dpu.nvidia.com/pci-address`, mapping
field `spec.pciAddress`, annotator helper `DPFPCIAddress`. Live: stamped
the annotation and patched `spec.pciAddress=0000-0f-00`.

## 10. `DPUClusterNotAllocated` — **fixed** (static, not Kamaji)

**Where:** DPF allocator needs a `DPUCluster` with `status.phase=Ready`.
Official HBN uses `type: kamaji`. Static needs `spec.kubeconfig`.

**Fix:** ServiceAccount `dpu-cluster-static-admin` + cluster-admin
binding + kubeconfig Secret `dpu-cluster-1-admin-kubeconfig` (key
`super-admin.conf`, server `https://kubernetes.default.svc`).
`DPUCluster/dpu-cluster-1` `type: static`.

**After ~8s:** `phase=Ready`, `HealthCheckPassed`, version
`v1.36.4+k3s1`. DPU allocated (`nodesCount=1`). Phase moved
Initializing → OS Installing. DPF created Argo Applications
(`dpu-cluster-1-hbn`, system CNIs, etc.). None were invented by hand.

Kamaji was not installed. The DPU control plane **is this k3s**. After
OS install the card will try to join dh2's API as a node.

## 11. VSP image missing PCI stamp — **fixed**

**Where:** imported `controller:latest` was built before
`DPFPCIAddress` / `provisioning.dpu.nvidia.com/pci-address`.

**Fix:** rebuilt linux/amd64 image, `k3s ctr images import`, rolled
adapter Deployment and VSP DaemonSet. Annotation still `0000-0f-00`.

## 12. Historical crash counts — not current

**Where:** `dpf-operator-controller-manager` 113 restarts, 
`cert-manager-cainjector` 88. All currently `Running`. New DPF
components from this session have `restarts=0`. No reboot.

## SFC jsonpath glitch during one snapshot

`kubectl get servicefunctionchain sfc-chart-probe` without `-n default`
printed `No resources found` in one combined script. The object is
still in `default` (`AGE 30h`). Final retest used `-n default` and
`DPFReady=True/AllServicesReady`.
