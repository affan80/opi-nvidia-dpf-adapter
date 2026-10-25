# Commands

Cwd for local commands: `opi-nvidia-dpf-adapter/`.

```bash
SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=25
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/opi_lab_key"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
)
```

## 1. Local

```bash
go test ./pkg/lifecycle/ -count=1
go test ./pkg/lifecycle/ -count=1 -v \
  -run 'TestReconcileCallsEnsureInstalled|TestParseIntent|TestRunPreflight|TestDetectingInstaller|TestEnsureInstalled'
GOTOOLCHAIN=go1.26.0 make lint
make verify-api
```

## 2. VPN and SSH

```bash
scutil --nc status "F5 Access" | head -n 8
scutil --nc start "F5 Access"          # if Disconnected
ping -c 2 -W 2000 172.22.1.2
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'hostname; k3s --version'
```

## 3. Live probes

```bash
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'k3s kubectl get dpuoperatorconfig dpu-operator-config -o yaml'
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl get crd dpus.provisioning.dpu.nvidia.com dscdevices.dpu.amd.com'
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl -n dpf-operator-system get deploy dpf-operator-controller-manager \
     -o jsonpath="{.spec.template.spec.containers[0].image}{"\n"}"'
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl get dpuoperatorconfig dpu-operator-config \
     -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{"\n"}{end}"'
ssh "${SSH_OPTS[@]}" root@172.22.1.2 \
  'k3s kubectl -n opi-nvidia-dpf-adapter-system logs \
     deploy/opi-nvidia-dpf-adapter-controller-manager --tail=80 | grep ensure'
```

Expected:

```
dpus.provisioning.dpu.nvidia.com   ...
Error ... dscdevices.dpu.amd.com not found

nvcr.io/nvidia/doca/dpf-system:v26.4.0

VendorOperatorSupported=True/Supported
# no Ready= line from the LCM

ensure installed vendor=nvidia source=spec action=detected
  installed=v26.4.0 skew=Supported ready=true
```

## 4. Image import (no Helm)

```bash
docker build --platform linux/amd64 -t controller:latest .
docker save controller:latest | gzip > /tmp/controller-ensure.tar.gz
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'mkdir -p /tmp/lab-job-ensure'
scp "${SSH_OPTS[@]}" /tmp/controller-ensure.tar.gz \
  root@172.22.1.2:/tmp/lab-job-ensure/controller.tar.gz
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'set -e
gzip -dc /tmp/lab-job-ensure/controller.tar.gz | k3s ctr images import -
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout restart \
  deploy/opi-nvidia-dpf-adapter-controller-manager
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout status \
  deploy/opi-nvidia-dpf-adapter-controller-manager --timeout=180s
rm -rf /tmp/lab-job-ensure
'
```
