# Preflight DPF dependencies

## Issue

`EnsureInstalled` treated a detected DPF CRD (`DPU`) plus an in-window
version pin as success. On lab host dh2 that produced:

```
ensure installed vendor=nvidia action=detected
  installed=v26.4.0 skew=Supported ready=true
VendorOperatorSupported=True/Supported
```

The same cluster was missing DPF v26.4 Helm prerequisites:

| Prerequisite | DPF table | Lab dh2 |
|---|---|---|
| Kubernetes | v1.36.4 pin | `v1.36.4+k3s1` present |
| cert-manager | required | CRDs + 3 deployments present |
| Argo CD | required (DPUService) | **absent** |
| Kamaji | conditional | **absent**, and no `DPUCluster` |

Reporting `Supported` / `detected` while Argo CD is missing is a false
installation success. The installer never checked cluster access, the
Kubernetes minor, or those charts.

## Approach

`pkg/lifecycle/dependency_checker.go` runs after the safe-config gate
and **before** the installer.

1. Cluster access: `ServerVersion` must succeed.
2. Kubernetes minor must be `v1.36` (`config/versions.yaml`).
   `v1.36.4+k3s1` is accepted; `v1.29` / `v1.37` are not.
3. NVIDIA required APIs: `certificates.cert-manager.io`,
   `applications.argoproj.io`.
4. Kamaji (`tenantcontrolplanes.kamaji.clastix.io`) only when a
   `DPUCluster` has `spec.type=kamaji`.
5. AMD: access + Kubernetes only. No DPF Helm charts.

A failed check sets `VendorOperatorSupported=False` reason `Blocked`,
skips the installer (`action=skipped`), and `AssessReadiness.Ready` is
false. The checker does not helm-install anything.

## How to unblock (operator, not this adapter)

These are cluster prerequisites. The LCM only reports them. Do not
helm-install Argo, cert-manager, or Kamaji from this adapter, and do
not change lab host packages to force `Supported`.

| Blocked on | What you see | How an operator fixes it |
|---|---|---|
| Missing Argo CD | `False/Blocked` `missing argo-cd` | Install Argo CD so `applications.argoproj.io` exists (DPF Helm prerequisites). Then the LCM reclassifies. |
| Missing cert-manager | `False/Blocked` `missing cert-manager` | Install cert-manager so `certificates.cert-manager.io` exists. |
| Kubernetes v1.29 (or any non-1.36 minor) | `False/Blocked` names both versions | Upgrade the cluster to Kubernetes 1.36.x. This lab is already `v1.36.4+k3s1`. |
| Unreachable API | `False/Blocked` `cannot reach the Kubernetes API` | Restore apiserver access / kubeconfig. The adapter does not retry an install while discovery fails. |
| Nil / missing discovery | `False/Blocked` `no cluster probe` | The manager must wire `discovery.NewDiscoveryClientForConfig` (`cmd/main.go` already does). |
| Kamaji, when a `DPUCluster` is `type=kamaji` | `False/Blocked` `missing kamaji` | Install Kamaji, or use a static `DPUCluster`. No `DPUCluster` on dh2 → not required. |

A missing prerequisite must stay `Blocked`. Installing the missing
chart on the lab just to turn the condition green is not a fix of this
adapter.

## Commands

```bash
# Unit: missing dep cannot look like success
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestCheckDependencies|TestEnsureInstalledBlocks|TestEnsureInstalledRunsWhen|TestSupportedKubernetes|TestAssessReadiness'

GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ ./pkg/mapping/ ./pkg/translation/ -count=1
GOTOOLCHAIN=go1.26.0 make lint
```

Lab inventory (read-only):

```bash
ssh "${SSH_OPTS[@]}" root@172.22.1.2 '
k3s kubectl version -o json
for c in certificates.cert-manager.io issuers.cert-manager.io \
         tenantcontrolplanes.kamaji.clastix.io applications.argoproj.io \
         dpus.provisioning.dpu.nvidia.com; do
  k3s kubectl get crd "$c" >/dev/null 2>&1 && echo PRESENT "$c" || echo ABSENT "$c"
done
k3s kubectl get deploy -A | grep -E "cert-manager|kamaji|argo|dpf-operator"
k3s kubectl get dpucluster -A
'
```

After deploying the rebuilt image, the live NVIDIA object must flip:

```bash
ssh "${SSH_OPTS[@]}" root@172.22.1.2 '
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason} {.message}{\"\\n\"}{end}"
k3s kubectl -n opi-nvidia-dpf-adapter-system logs \
  deploy/opi-nvidia-dpf-adapter-controller-manager --tail=40 | grep "ensure installed"
'
```

Want: `VendorOperatorSupported=False/Blocked` and
`missing argo-cd`. Must **not** be `True/Supported` with
`action=detected` while Argo is absent.

Lab after deploy (2026-10-23, before prereq charts):

```
VendorOperatorSupported=False/Blocked
  DPF v26.4.0 is blocked: missing argo-cd
```

After installing DPF Helm prerequisites on dh2 (Argo CD 10.2.2,
NFD 0.19.0, maintenance-operator 0.3.0 in `dpf-operator-system`):

```
VendorOperatorSupported=True/Supported
ensure installed vendor=nvidia action=detected skew=Supported ready=true
  kubernetes=v1.36.4+k3s1 missing=
```

Kamaji is still absent. There is no `DPUCluster` of type `kamaji`, so
it stays not required. LCM does not write `Ready`.
