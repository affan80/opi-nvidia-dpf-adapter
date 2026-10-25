# Testing commands — local gates and live lab

Cwd for local commands: `opi-nvidia-dpf-adapter/`

`SSH_OPTS` for every dh2 command is in [01_lab_connection.md](01_lab_connection.md).
Copy it once in the shell:

```bash
SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=25
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
)
```

Results below are from 2026-10-23 on this Mac + dh2.

------------------------------------------------
A. Start the lab
------------------------------------------------

```bash
scutil --nc status "F5 Access" | head -n 8
# if Disconnected:
scutil --nc start "F5 Access"

ping -c 2 -W 2000 <your-lab-hosts>
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s --version; hostname'
```

Result 2026-10-23
```
Connected
2 packets transmitted, 2 packets received, 0.0% packet loss
k3s version v1.36.4+k3s1
dh2
```

------------------------------------------------
B. Local schema / API gates
------------------------------------------------

```bash
# 1. Official pin vs fixtures vs mappings
make verify-api

# 2. FieldMapping JSON schema vs Go structs
go test ./pkg/mapping/ -count=1 -v -run \
  'TestSchemaMatchesGoStructs|TestSchemaReservesReadyCondition|TestShippedMappingsDeclareTheSchema'

# 3. Writer table vs vendored CRD status
go test ./pkg/mapping/ -count=1 -v -run \
  'TestWriterTableSchemaAgreesWithVendoredCRDs|TestShippedMappingsMatchWriterTable|TestValidateRequiresStatusSchema'

# 4. Ownership graph
go test ./pkg/mapping/ -count=1 -v -run \
  'TestSFCMappingIsDirectOwnership|TestNoTwoMappingsOwnTheSameDPFKind|TestSFCApplyEmitsOwnedGraph'

# 5. Design contract still records the pins
go test ./pkg/mapping/ -count=1 -run TestHybridDesignDocIsTheContract

# 6. NVIDIA window + vendor sanitizer
go test ./pkg/lifecycle/ -count=1 -v -run \
  'TestPinnedNVIDIAWindowMatchesVersionsYAML|TestSupportedRangeIsWellFormed|TestClassifySkew|TestKnownVendor|TestSanitizeVendorTokenRejectsInjection|TestResolveVendorPrefersSpecThenAnnotation'

# 7. AMD fake-device guard
go test ./pkg/mapping/ -count=1 -v -run TestAMDMappingRequiresSerial

# 8. Constrained spec.vendor on the fixture
go test ./pkg/mapping/ -count=1 -v -run TestDpuOperatorConfigVendorIsConstrained

# 9. RBAC vs emit kinds
make verify-rbac

# 10. Full packages (envtest 1.36)
KUBEBUILDER_ASSETS="$(pwd)/bin/k8s/1.36.0-darwin-arm64" \
  go test ./pkg/mapping/ ./pkg/lifecycle/ ./pkg/translation/ ./test/conformance/ -count=1
```

Result 2026-10-23
```
make verify-api     PASS
make verify-rbac    PASS  (zz_generated_rbac.go up to date)
ok  pkg/mapping
ok  pkg/lifecycle
ok  pkg/translation
ok  test/conformance   (uses pinned fixtures, not sibling checkouts)
```

------------------------------------------------
C. Live cluster — health
------------------------------------------------

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'bash -s' <<'EOF'
k3s kubectl get node -o wide
k3s kubectl -n dpf-operator-system get deploy -o \
  jsonpath='{range .items[*]}{.metadata.name}={.spec.template.spec.containers[0].image}{"\n"}{end}'
k3s kubectl -n opi-nvidia-dpf-adapter-system get pods -o wide
k3s kubectl -n opi-nvidia-dpf-adapter-system logs \
  deploy/opi-nvidia-dpf-adapter-controller-manager --tail=40 \
  | grep -E 'ERROR|unknown field|not declared|Forbidden' || echo NO_ADAPTER_ERRORS
k3s kubectl -n opi-nvidia-dpf-adapter-system get cm \
  opi-nvidia-dpf-adapter-mappings -o json \
  | python3 -c 'import json,sys; print(sorted(json.load(sys.stdin)["data"].keys()))'
EOF
```

Result 2026-10-23
```
dh2 Ready  v1.36.4+k3s1
dpf-operator-controller-manager=nvcr.io/nvidia/doca/dpf-system:v26.4.0
controller-manager  1/1 Running
nvidia-vsp          1/1 Running
NO_ADAPTER_ERRORS
['dataprocessingunit.yaml', 'servicefunctionchain.yaml']
```

------------------------------------------------
D. Live cluster — CRD field matrix
------------------------------------------------

For every path the mappings write, the live OpenAPI must have the property.

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'python3 -' <<'PY'
import json, subprocess

def schema(name):
    out = subprocess.check_output(["k3s","kubectl","get","crd",name,"-o","json"])
    obj = json.loads(out)
    v = next(x for x in obj["spec"]["versions"] if x.get("storage"))
    return v["schema"]["openAPIV3Schema"]

def has(schema, dotted):
    cur = schema
    for p in dotted.split("."):
        props = (cur or {}).get("properties") or {}
        if p not in props:
            return False
        cur = props[p]
    return True

need = {
  "dpuoperatorconfigs.config.openshift.io": ["spec.vendor","status.conditions"],
  "dataprocessingunits.config.openshift.io": ["spec.nodeName","status.conditions"],
  "servicefunctionchains.config.openshift.io": ["spec.networkFunctions","status.conditions"],
  "dpudevices.provisioning.dpu.nvidia.com": ["spec.serialNumber"],
  "dpuflavors.provisioning.dpu.nvidia.com": ["spec.dpuMode"],
  "bfbs.provisioning.dpu.nvidia.com": ["spec.url"],
  "dpus.provisioning.dpu.nvidia.com": [
      "spec.dpuNodeName","spec.dpuDeviceName","spec.dpuFlavor",
      "spec.bfb","spec.serialNumber","spec.nodeEffect"],
  "dpuservices.svc.dpu.nvidia.com": [
      "spec.serviceID","spec.helmChart","spec.interfaces","spec.paused","spec.security"],
  "dpuserviceinterfaces.svc.dpu.nvidia.com": ["spec.template"],
  "dpuservicechains.svc.dpu.nvidia.com": ["spec.template"],
}
dead = []
for crd, fields in need.items():
    s = schema(crd)
    for f in fields:
        ok = has(s, f)
        print(("LIVE" if ok else "DEAD"), crd, f)
        if not ok:
            dead.append((crd, f))
print("AMD CRDs:")
crd = subprocess.check_output(["k3s","kubectl","get","crd"]).decode()
print("NONE" if "amd" not in crd.lower() and "dsc" not in crd.lower() else crd)
print("DEAD", dead or "NONE")
PY
```

Result 2026-10-23: every mapped path LIVE. AMD CRDs NONE.

------------------------------------------------
E. Live cluster — server dry-run of each mapped write
------------------------------------------------

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl apply --dry-run=server -f -' <<'Y'
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata: {name: lab-dry-vendor}
spec: {logLevel: 2, vendor: nvidia}
Y

# must fail
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl apply --dry-run=server -f -' <<'Y'
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata: {name: lab-dry-bad-vendor}
spec: {logLevel: 2, vendor: acme}
Y

ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl apply --dry-run=server -f -' <<'Y'
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata: {name: lab-dry-svc, namespace: default}
spec:
  serviceID: lab-dry-svc
  helmChart:
    source: {repoURL: https://helm.ngc.nvidia.com/nvidia/doca, chart: hbn, version: v25.10.1}
  interfaces: [hbn-ifc]
  paused: false
  security: {privileged: true}
Y
```

Result 2026-10-23
```
dpuoperatorconfig .../lab-dry-vendor created (server dry run)
The DpuOperatorConfig "lab-dry-bad-vendor" is invalid:
  spec.vendor: Unsupported value: "acme": supported values: "nvidia", "amd"
dpuservice .../lab-dry-svc created (server dry run)
```

Also dry-ran (all created): SFC chart NF, DPUDevice serial, DPUFlavor
dpuMode, BFB url, DPU mapped spec, DPUServiceInterface template,
DPUServiceChain switches.

------------------------------------------------
F. Live cluster — objects the translator already wrote
------------------------------------------------

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'bash -s' <<'EOF'
echo "=== OPI ==="
k3s kubectl get dpuoperatorconfig dpu-operator-config -o yaml | sed -n '/^spec:/,/^status:/p;/^status:/,$p'
k3s kubectl get dataprocessingunit dh2-bluefield-2 -o wide
k3s kubectl get dataprocessingunit dh2-bluefield-2 -o jsonpath='{.status.conditions}{"\n"}'
k3s kubectl -n default get servicefunctionchain sfc-chart-probe -o jsonpath='{.spec}{"\n"}{.status}{"\n"}'

echo "=== DPF children ==="
k3s kubectl -n dpf-operator-system get dpu,dpudevice,dpuflavor,bfb -o wide
k3s kubectl -n default get dpuservice,dpuserviceinterface,dpuservicechain -o wide

echo "=== written fields ==="
k3s kubectl -n default get dpuservice hbn -o jsonpath='chart={.spec.helmChart.source} priv={.spec.security.privileged}{"\n"}'
k3s kubectl -n default get dpuserviceinterface hbn-ifc -o jsonpath='{.spec.template.spec.template.spec}{"\n"}'
k3s kubectl -n dpf-operator-system get dpu dh2-bluefield-2 -o jsonpath='{.spec}{"\n"}phase={.status.phase}{"\n"}'
EOF
```

Result 2026-10-23

| Object | Result |
|---|---|
| `dpu-operator-config` | `spec.vendor=nvidia`, `VendorOperatorSupported=True` |
| `dh2-bluefield-2` (OPI) | BlueField-2, node dh2, `DPFReady=False` (children Initializing) |
| `sfc-chart-probe` | chart hbn v25.10.1, `DPFReady=True AllServicesReady` |
| DPUDevice | serial `MT2321XZ0KVX` |
| Flavor | `dpuMode=dpu` |
| BFB | mellanox bf-bundle URL, phase Initializing |
| DPU | all six mapped spec fields, phase Initializing |
| DPUService/hbn | chart hbn v25.10.1, `privileged=true` |
| hbn-ifc | `interfaceType=service` `interfaceName=net0` `network=default` |
| chain | one switch, `matchLabels.service=hbn` |

`phase=Initializing` is DPF runtime, not a dead write.

------------------------------------------------
G. Live cluster — annotation unknown vendor
------------------------------------------------

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'bash -s' <<'EOF'
k3s kubectl apply -f - <<'Y'
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata:
  name: vendor-ann-acme
  annotations:
    lifecycle.opi.nvidia.com/vendor: acme
spec:
  logLevel: 2
Y
sleep 3
k3s kubectl get dpuoperatorconfig vendor-ann-acme -o jsonpath='{.status.conditions}{"\n"}'
k3s kubectl delete dpuoperatorconfig vendor-ann-acme
EOF
```

Result 2026-10-23
```
type=VendorOperatorSupported status=False reason=UnknownVendor
message=vendor "acme" is not known; allowed values are nvidia, amd
```

------------------------------------------------
H. Pass / fail for this run
------------------------------------------------

| Gate | Result |
|---|---|
| VPN + SSH | Connected, 0% loss |
| Local verify-api / verify-rbac / packages | PASS |
| Live CRD mapped paths | all LIVE |
| Live dry-run mapped writes | all created |
| Live dry-run bad vendor | rejected |
| Live objects have mapped spec fields | yes |
| SFC DPFReady | True |
| NVIDIA VendorOperatorSupported | True |
| AMD stub CRDs on dh2 | absent |
| Adapter errors | none |
| Dead APIs | NONE |
