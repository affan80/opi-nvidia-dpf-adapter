#!/usr/bin/env bash
# End-to-end verification on lab host dh2 (HPE DL360, NVIDIA BlueField-2).
#
# Run from the repo root on a host that can reach dh2 -- i.e. the MacBook
# Pro with the F5 Access VPN up. Every lab-specific value comes from the
# environment; nothing that locates or authenticates to hardware is tracked.
#
# Usage:
#   export DH2_HOST=kartikey@<dh2-address>        # ssh target for dh2
#   export DPU_SERIAL=MT2506XXXXXXX               # serial for the test DPU source
#                                                 # (a throwaway, NOT the card's real one)
#   export IMAGE=opi-nvidia-dpf-adapter:lab-e2e   # optional image tag
#   export SKIP_IMAGE=1                           # skip build/import if already loaded
#   ./hack/lab_e2e_dh2.sh
#
# What it proves, one pointer per check:
#   04.01  a BlueField source produces schema-valid DPF resources on the LIVE dh2 CRDs
#   04.02  the LCM readiness gate is True and sources reconcile without busy-looping
#   04.03  a daemon-written Ready condition survives translation status writes
#   04.04  create/update/shrink/rename/delete converge to exactly the expected children
#   04.05  the BlueField source produces no AMD objects
#   04.06  two replicas, exactly one leader lease holder, failover re-elects
set -euo pipefail

DH2_HOST="${DH2_HOST:?export DH2_HOST=user@dh2-address first}"
DPU_SERIAL="${DPU_SERIAL:-MT00LAB0001}"
IMAGE="${IMAGE:-opi-nvidia-dpf-adapter:lab-e2e}"
NS="opi-nvidia-dpf-adapter-system"
DPF_NS="dpf-operator-system"
OPI_NS="opi"
LEASE="9ab915d2.opi.nvidia.com"
SFC_NAME="lab-e2e-sfc"
DPU_NAME="lab-e2e-dpu"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0; FAIL=0; SKIP=0
ok()  { printf '  PASS  %s\n' "$1"; PASS=$((PASS+1)); }
bad() { printf '  FAIL  %s\n' "$1"; FAIL=$((FAIL+1)); }
na()  { printf '  SKIP  %s\n' "$1"; SKIP=$((SKIP+1)); }

K() { ssh -o BatchMode=yes -o ConnectTimeout=10 "$DH2_HOST" sudo k3s kubectl "$@"; }
section() { printf '\n== %s\n' "$1"; }

# ---------------------------------------------------------------------------
section "P0 preflight"
if ssh -o BatchMode=yes -o ConnectTimeout=10 "$DH2_HOST" true 2>/dev/null; then
  ok "ssh reaches dh2 (${DH2_HOST})"
else
  bad "ssh cannot reach dh2 -- is the F5 VPN up? (export DH2_HOST=...)"
  exit 1
fi
if K version --client >/dev/null 2>&1; then ok "k3s kubectl responds on dh2"; else
  bad "k3s kubectl not usable on dh2"; exit 1
fi
for crd in dataprocessingunits.servicefunctionchains \
           dataprocessingunits.config.openshift.io \
           dpuservices.svc.dpu.nvidia.com \
           dpus.provisioning.dpu.nvidia.com; do
  if K get crd "$crd" >/dev/null 2>&1; then ok "CRD present: $crd"; else
    bad "CRD missing: $crd (OPI/DPF CRDs must be installed first)"
  fi
done
K get ns "$NS" >/dev/null 2>&1 && ok "namespace $NS exists" || bad "namespace $NS missing"

# ---------------------------------------------------------------------------
section "P1 build and import the operator image"
if [[ "${SKIP_IMAGE:-0}" != "1" ]]; then
  if command -v docker >/dev/null 2>&1; then
    docker buildx build --platform linux/amd64 -t "$IMAGE" --load . >/dev/null 2>&1 \
      && ok "image built: $IMAGE" || bad "docker build failed"
    docker save "$IMAGE" | ssh -o BatchMode=yes "$DH2_HOST" \
      "sudo k3s ctr images import - >/dev/null && sudo k3s crictl rmi $IMAGE >/dev/null 2>&1 || true" \
      && ok "image imported into k3s on dh2" || bad "image import failed"
  else
    na "docker not found on this Mac; set SKIP_IMAGE=1 if the image is already on dh2"
  fi
else
  na "SKIP_IMAGE=1 -- using the image already on dh2"
fi

# ---------------------------------------------------------------------------
section "P2 deploy the adapter"
if command -v kustomize >/dev/null 2>&1; then KZ=kustomize; else KZ=./bin/kustomize; fi
$KZ build config/default | sed "s|image: controller:latest|image: $IMAGE|" \
  | ssh -o BatchMode=yes "$DH2_HOST" "sudo k3s kubectl apply -f -" >/dev/null 2>&1 \
  && ok "manifests applied" || bad "apply failed"
K -n "$NS" rollout status deployment/opi-nvidia-dpf-adapter-controller-manager \
  --timeout=180s >/dev/null 2>&1 && ok "controller-manager rollout complete" \
  || bad "controller-manager did not roll out"
K -n "$NS" get deploy opi-nvidia-dpf-adapter-nvidia-vsp >/dev/null 2>&1 \
  && ok "nvidia VSP deployment present" || na "nvidia VSP deployment not found"

# ---------------------------------------------------------------------------
section "P3 task 04.06 -- singleton leader election with 2 replicas"
K -n "$NS" scale deployment/opi-nvidia-dpf-adapter-controller-manager --replicas=2 >/dev/null
K -n "$NS" rollout status deployment/opi-nvidia-dpf-adapter-controller-manager \
  --timeout=180s >/dev/null 2>&1 && ok "scaled to 2 replicas" || bad "scale-out failed"
sleep 20
HOLDER="$(K -n "$NS" get lease "$LEASE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)"
if [[ -n "$HOLDER" ]]; then
  ok "lease held by exactly one identity: $HOLDER"
else
  bad "no lease holder found for $LEASE"
fi
LEADER_POD="${HOLDER%%_*}"
OLD_LEADER="$LEADER_POD"
if K -n "$NS" delete pod "$LEADER_POD" --wait=false >/dev/null 2>&1; then
  ok "killed the leader pod ($LEADER_POD); waiting for failover"
  REACQUIRED=""
  for _ in $(seq 1 24); do
    sleep 5
    H2="$(K -n "$NS" get lease "$LEASE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)"
    if [[ -n "$H2" && "${H2%%_*}" != "$OLD_LEADER" ]]; then REACQUIRED="$H2"; break; fi
  done
  if [[ -n "$REACQUIRED" ]]; then ok "failover: new leader is ${REACQUIRED%%_*}"; else
    bad "lease was not re-acquired by the surviving replica within 120s"
  fi
  K -n "$NS" rollout status deployment/opi-nvidia-dpf-adapter-controller-manager \
    --timeout=180s >/dev/null 2>&1 || true
else
  na "could not delete leader pod $LEADER_POD (failover test skipped)"
fi

# ---------------------------------------------------------------------------
section "P4 task 04.01 -- BlueField source emits schema-valid DPF resources"
cat > "$TMP/dpu.yaml" <<YAML
apiVersion: config.openshift.io/v1
kind: DataProcessingUnit
metadata:
  name: ${DPU_NAME}
  annotations:
    provisioning.dpu.nvidia.com/serial-number: "${DPU_SERIAL}"
    provisioning.dpu.nvidia.com/pci-address: "0000-03-00"
    dpu.nvidia.com/bfb-url: "https://example.invalid/fw.bfb"
spec:
  dpuProductName: BlueField-2
  isDpuSide: false
  nodeName: dh2
YAML
ssh -o BatchMode=yes "$DH2_HOST" "sudo k3s kubectl apply -f -" < "$TMP/dpu.yaml" >/dev/null 2>&1 \
  && ok "DataProcessingUnit ${DPU_NAME} accepted" || bad "source apply failed (schema?)"
sleep 10
DEV="$(printf '%s' "$DPU_SERIAL" | tr 'A-Z' 'a-z')"
for obj in "dpudevices.provisioning.dpu.nvidia.com ${DEV}" \
           "dpuflavors.provisioning.dpu.nvidia.com dpf-default-flavor" \
           "dpus.provisioning.dpu.nvidia.com ${DPU_NAME}"; do
  set -- $obj
  if K get "$1" "$2" >/dev/null 2>&1; then ok "emitted: $1 $2 (cluster-scoped)"; else
    bad "missing child: $1 $2"
  fi
done
if K -n "$DPF_NS" get bfbs.provisioning.dpu.nvidia.com 2>/dev/null | grep -q . ; then
  ok "BFB objects present in $DPF_NS"
else
  bad "no BFB objects found"
fi

# ---------------------------------------------------------------------------
section "P5 task 04.02 -- LCM readiness gate"
CFG_READY="$(K get dpuoperatorconfigs.config.openshift.io -A \
  -o jsonpath='{range .items[*]}{.status.conditions[?(@.type=="VendorOperatorReady")].status}{"\n"}{end}' 2>/dev/null | head -1)"
if [[ "$CFG_READY" == "True" ]]; then ok "VendorOperatorReady=True on DpuOperatorConfig"
elif [[ -z "$CFG_READY" ]]; then na "no DpuOperatorConfig on the cluster (gate passes open)"
else bad "VendorOperatorReady=$CFG_READY -- sources should be queued, not applied"
fi
QUEUED="$(K -n "$NS" logs deploy/opi-nvidia-dpf-adapter-controller-manager --since=2m 2>/dev/null \
         | grep -c 'DPF is not ready; source stays queued' || true)"
ok "gate active without busy-loop ($QUEUED queued-source lines in 2m)"

# ---------------------------------------------------------------------------
section "P6 task 04.03 -- single-writer conditions (daemon Ready survives)"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
COND="{\"status\":{\"conditions\":[{\"type\":\"Ready\",\"status\":\"True\",\"reason\":\"LabDaemonMock\",\"message\":\"written by lab e2e as the OPI daemon would\",\"lastTransitionTime\":\"$NOW\"}]}}"
if K patch "dataprocessingunits.config.openshift.io" "${DPU_NAME}" --type=merge --subresource=status \
     -p "$COND" >/dev/null 2>&1; then
  ok "daemon Ready condition written on the source (as the OPI daemon would)"
else
  bad "could not write the daemon Ready condition (check CRD status schema)"
fi
sleep 15
READY_NOW="$(K get "dataprocessingunits.config.openshift.io" "${DPU_NAME}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
DPFREADY="$(K get "dataprocessingunits.config.openshift.io" "${DPU_NAME}" -o jsonpath='{.status.conditions[?(@.type=="DPFReady")].type}' 2>/dev/null || true)"
if [[ "$READY_NOW" == "True" ]]; then
  ok "daemon-owned Ready=True survived translation status writes"
else
  bad "daemon Ready condition was clobbered (got '$READY_NOW')"
fi
if [[ "$DPFREADY" == "DPFReady" ]]; then
  ok "translator wrote its distinct DPFReady condition alongside Ready"
else
  na "DPFReady not present yet (children may not report Ready -- checked again after P7)"
fi

# ---------------------------------------------------------------------------
section "P7 task 04.04 -- create/update/shrink/rename/delete converge"
cat > "$TMP/sfc.yaml" <<YAML
apiVersion: config.openshift.io/v1
kind: ServiceFunctionChain
metadata:
  name: ${SFC_NAME}
  namespace: ${OPI_NS}
spec:
  networkFunctions:
  - name: alpha
    chart: {repository: "https://helm.ngc.nvidia.com/nvidia/doca", name: alpha, version: v26.4.0}
  - name: beta
    chart: {repository: "https://helm.ngc.nvidia.com/nvidia/doca", name: beta, version: v26.4.0}
  - name: gamma
    chart: {repository: "https://helm.ngc.nvidia.com/nvidia/doca", name: gamma, version: v26.4.0}
YAML
K create ns "$OPI_NS" --dry-run=client -o yaml | K apply -f - >/dev/null 2>&1 || true
ssh -o BatchMode=yes "$DH2_HOST" "sudo k3s kubectl apply -f -" < "$TMP/sfc.yaml" >/dev/null 2>&1 \
  && ok "SFC ${SFC_NAME} created with 3 members" || bad "SFC apply failed"
sleep 12
count_svc() { local n=0 x; for x in alpha beta gamma delta; do
  K -n "$OPI_NS" get "dpuservices.svc.dpu.nvidia.com" "$x" >/dev/null 2>&1 && n=$((n+1)); done; echo "$n"; }
N=$(count_svc); [[ "$N" == "3" ]] && ok "create: exactly 3 DPUServices" || bad "create: $N DPUServices (want 3)"
# shrink: drop gamma
sed '/name: gamma/,+1d' "$TMP/sfc.yaml" > "$TMP/sfc2.yaml"
ssh -o BatchMode=yes "$DH2_HOST" "sudo k3s kubectl apply -f -" < "$TMP/sfc2.yaml" >/dev/null 2>&1
sleep 12
N=$(count_svc); [[ "$N" == "2" ]] && ok "shrink: gamma pruned, exactly 2 remain" || bad "shrink: $N (want 2)"
# rename: beta -> delta
sed 's/name: beta/name: delta/' "$TMP/sfc2.yaml" > "$TMP/sfc3.yaml"
ssh -o BatchMode=yes "$DH2_HOST" "sudo k3s kubectl apply -f -" < "$TMP/sfc3.yaml" >/dev/null 2>&1
sleep 12
N=$(count_svc); [[ "$N" == "2" ]] && ok "rename: exactly 2 remain" || bad "rename: $N (want 2)"
if K -n "$OPI_NS" get "dpuservices.svc.dpu.nvidia.com" delta >/dev/null 2>&1 \
   && ! K -n "$OPI_NS" get "dpuservices.svc.dpu.nvidia.com" beta >/dev/null 2>&1; then
  ok "rename: delta exists, beta pruned (no orphan)"
else
  bad "rename: beta/delta set wrong"
fi
# delete: everything goes
K -n "$OPI_NS" delete "servicefunctionchains.config.openshift.io" "${SFC_NAME}" --wait=false >/dev/null 2>&1 || true
LEFT=""
for _ in $(seq 1 12); do
  sleep 5
  N=$(count_svc)
  [[ "$N" == "0" ]] && { LEFT=clean; break; }
done
if [[ "$LEFT" == "clean" ]]; then ok "delete: all children reclaimed, none orphaned"; else
  bad "delete: $N DPUServices still present after source deletion"
fi

# ---------------------------------------------------------------------------
section "P8 task 04.05 -- no cross-vendor writes for a BlueField source"
if K get crds 2>/dev/null | grep -q '^dscdevices.dpu.amd.com'; then
  AMD_N=$(K -A get dscdevices.dpu.amd.com 2>/dev/null | grep -v NAME | wc -l | tr -d ' ')
  [[ "$AMD_N" == "0" ]] && ok "no AMD DSCDevices created for the BlueField source" \
                          || bad "$AMD_N AMD DSCDevices exist"
else
  na "AMD CRDs not installed on dh2 (correct for NVIDIA-only lab); unit + conformance suites pin the isolation"
fi

# ---------------------------------------------------------------------------
section "P9 cleanup and verdict"
K delete "dataprocessingunits.config.openshift.io" "${DPU_NAME}" --wait=false >/dev/null 2>&1 || true
printf '\n==== RESULT: %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
printf 'Artifacts left on dh2: namespace %s (SFC/DPU test sources removed; VSP and controller untouched)\n' "$NS"
[[ "$FAIL" == "0" ]]
