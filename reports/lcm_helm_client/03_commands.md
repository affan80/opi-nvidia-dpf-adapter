# Commands and versions

## Versions used this run

| Tool / pin | Version |
|---|---|
| Go (local toolchain) | `go1.26.0` (`GOTOOLCHAIN=go1.26.0`) |
| k3s on dh2 | `v1.36.4+k3s1` (4dedb15b) |
| Kubernetes API | `v1.36.4` (client + server) |
| Kustomize (bundled) | `v5.8.1` |
| Go on dh2 (k3s) | `go1.26.7` |
| Kubernetes Go modules | `v0.36.0` |
| controller-runtime | `v0.24.1` |
| DPF operator image | `nvcr.io/nvidia/doca/dpf-system:v26.4.0` |
| DPF Helm chart pin | `dpf-operator` `v26.4.0` |
| Chart repo | `https://helm.ngc.nvidia.com/nvidia/doca` |
| Release / namespace | `dpf-operator` / `dpf-operator-system` |
| Owner (adapter) | `opi-nvidia-dpf-adapter` |
| Live Helm secret owner | `helm` (unmanaged) |
| Adapter image on lab | `controller:latest` |
| Ubuntu on dh2 | 26.04.1 LTS, kernel 7.0.0-38-generic |

## 1. VPN + SSH (this Mac is the VPN Mac)

```bash
scutil --nc start "F5 Access"
# wait until first line is Connected
scutil --nc status "F5 Access" | head -1

SSH_OPTS=(
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/opi_lab_key"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
  -o ConnectTimeout=20
)
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'hostname; k3s --version; date -u'
```

Do not hop through another Mac. Do not install F5 on another Mac.
Do not copy `opi_lab_key`. Do not change `/etc`, packages, or systemd.

This run: F5 started, Connected after ~90s, ping 0% loss, `SSH_OK` / `dh2`.

## 2. Prerequisite inventory (read-only)

```bash
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'bash -s' <<'EOF'
k3s --version
k3s kubectl version
k3s kubectl get crd certificates.cert-manager.io applications.argoproj.io \
  nodefeatures.nfd.k8s-sigs.io nodemaintenances.maintenance.nvidia.com \
  dpus.provisioning.dpu.nvidia.com
k3s kubectl get pods -n cert-manager
k3s kubectl get pods -n dpf-operator-system
k3s kubectl get secret -n dpf-operator-system -l owner=helm --show-labels
k3s kubectl get dpuoperatorconfig -o jsonpath='{range .items[*].status.conditions[*]}{.type}={.status}/{.reason}{"\n"}{end}'
k3s kubectl get bfb,dpudevice,dpunode,dpu,dpucluster -n dpf-operator-system
k3s kubectl get applications -n dpf-operator-system
EOF
```

The `helm` CLI is **not** installed on dh2. Do not `apt install helm`.
Read the release from the Helm secret (`owner=helm`).

## 3. Unit tests (done-when)

```bash
cd /Users/kartikey0104/Desktop/opi-dpf-adapter/opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestHelmFresh|TestHelmAlready|TestHelmFreshAndAdopted|TestHelmAdopts|TestHelmDoesNotAdopt|TestHelmUpgrade|TestSafeHelm|TestHelmInstallerAMD|TestEnsureInstalledHelm|TestHelmClientSource'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

## 4. What not to run on the lab

```bash
# FORBIDDEN
helm uninstall dpf-operator -n dpf-operator-system
helm upgrade dpf-operator ...
helm install dpf-operator ...
```

Leave `LifecycleManager.Helm` nil in `cmd/main.go` until a release is
explicitly owned by `opi-nvidia-dpf-adapter`.
