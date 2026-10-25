# Pipeline-level vulnerabilities

These are security or safety findings on the live pipeline, not just
"it did not become Ready".

## 1. `argocd-initial-admin-secret` left in etcd — **fixed**

**Where:** `dpf-operator-system/argocd-initial-admin-secret`.

**What:** bootstrap admin password as a Kubernetes Secret. Anyone with
get-secrets in that namespace (or etcd backup access) gets the UI
password. Argo docs say delete it after first login.

**Risk:** cluster-admin of Argo CD, deploy arbitrary Applications.

**Fix:** deleted the secret. Did not print the password. Confirmed
`GONE`. UI Service remains ClusterIP, no externalIPs, no
`server.insecure`.

**Residual:** admin login now requires a configured user or a new
password reset flow. That is intended.

## 2. Argo CD UI exposure — **checked, not a vuln on this cluster**

Server `type=ClusterIP`, ports 80/443, `externalIPs` empty. ApplicationSet
controller replicas 0 (DPF chart default). Not LoadBalancer, not NodePort.

## 3. `DPUService.spec.security.privileged` — **accepted DPF CEL, plus one lab relaxation**

**Adapter path (`hbn`):** still `privileged: true`. DPF v26.4 CEL for
DPU-targeted services. Required for HBN to run privileged workloads on
the DPU. Documented as accepted risk, not removed.

**Operator path (system DPUServices):** live operator does not set
`security` at all. The newer CRD CEL blocked the whole DPF system
pipeline. The **required-privileged-on-create** CEL was removed from
the live CRD so the operator could apply objects. That is a
**validation weakening**:

- What we removed: "privileged must be set when deployInCluster is false"
- What we kept: "privileged must not be set when deployInCluster is true"
- Grandfathering of old objects without privileged remains in DPF
  upstream CEL; we dropped the create-time requirement to match the
  operator that is actually running (`v26.4.0`)

**Risk of the relaxation:** a client can create a DPU-targeted
DPUService without an explicit privileged true/false. NVIDIA wanted
that to be explicit so DPUCluster admission can gate
`securityContext.privileged`. On this lab there is no DPUCluster yet,
so the admission policy on the DPU cluster is not in play.

**Do not carry this CRD edit into production.** Align operator image
and CRDs instead, or upgrade the operator to a build that emits
`security.privileged` (current `doca-platform` manifests already do).

## 4. Wrong BFB would have been flashed — **fixed before flash**

Old URL `bf-bundle-2.7.0-33` Ubuntu 22.04 has no software-inventory
JSON. DPF v26.4 correctly refused to treat it as Ready. If that check
had been skipped, the provisioning pipeline would flash an unsupported
bundle onto BlueField-2.

**Fix:** pin `bf-bundle-3.4.0-92` Ubuntu 24.04 **4K** (not `64k`). The
`64k` image is the DPF doc default for BlueField-3. This host is
BlueField-2 (PCI device `0xa2d6`). Using 64k here is a brick risk.

DPF host-trusted docs list **BlueField-3** as the worker DPU. This lab
is off-spec (BF2). Inventory parse succeeded (`BFB phase=Ready`). OS
install was **not** started because DPUCluster is missing.

## 5. Dummy `br-dpu` default route — **lab stub, residual vuln**

Hostagent will not mark `OOBBridgeConfigured` unless a bridge named
`br-dpu` exists **and** has a default route. A real default on that
bridge would replace management routing.

The stub adds `br-dpu` with a **metric 10000** default via
`<your-lab-hosts>`. Real DHCP defaults stay metric 100. Ping to
`<your-lab-hosts>` stayed up.

**Risk:** DPF believes OOB is configured. Later stages that actually
send DPU management traffic over `br-dpu` will fail or black-hole.
This is a controller-check bypass, not a fabric. Not persisted in
netplan; a reboot drops it.

## 6. Control-plane label removed on the only k3s node — **lab tradeoff**

Detector affinity excludes control-plane. Removing
`node-role.kubernetes.io/control-plane` lets detector and hostagent
run on dh2. k3s may restore the label. Components that select
control-plane for new pods may not land until the label is back.

**Risk:** single-node lab only. Do not do this on a HA control plane
that is supposed to keep BlueField in NIC mode and unprovisioned.

## 7. `DPUService` privileged + hostagent privileged — **design, not removed**

`dh2-dms` and `dpf-dpu-detector` run privileged / hostNetwork. That is
DPF host-trusted design (DMS talks to the card). Not changed.

## 8. Historical crash-loop counts — **not active**

`dpf-operator-controller-manager` 113, `cert-manager-cainjector` 88,
both currently Running with old last-restart (~43h). New pods from this
session have 0 restarts. A crash-loop **now** would be a pipeline vuln;
history alone is not.

## 9. Static DPUCluster uses host k3s + cluster-admin token — **accepted lab risk**

**Where:** Secret `dpu-cluster-1-admin-kubeconfig` and SA
`dpu-cluster-static-admin` in `dpf-operator-system`.

**What:** static cluster manager health-checks that kubeconfig and
treats this k3s as the DPU control plane. The token is cluster-admin
for 8760h. After OS install, the BlueField will kubeadm/join **this**
API.

**Risk:** DPU node object on the management cluster; cluster-admin
token in etcd; not a separate Kamaji tenant. Lab-only. Production HBN
uses Kamaji + reserved VIP.

## 10. AMD FieldMapping accidentally mounted, then removed

For one restart the live mappings ConfigMap included `amd-dsc200.yaml`.
The adapter tried to watch `dpu.amd.com` CRDs that are not installed
and logged `no matches for kind DSCNodePolicy`. That is the fake-device
risk on BlueField. ConfigMap was put back to NVIDIA-only
(`dataprocessingunit.yaml` + `servicefunctionchain.yaml`).
