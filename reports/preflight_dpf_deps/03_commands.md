# Commands

```bash
# --- this Mac ---
cd opi-nvidia-dpf-adapter

GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestCheckDependencies|TestEnsureInstalledBlocks|TestEnsureInstalledRunsWhen|TestReconcilePublishesBlocked|TestSupportedKubernetes'

GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ ./pkg/mapping/ ./pkg/translation/ -count=1
GOTOOLCHAIN=go1.26.0 make lint
GOTOOLCHAIN=go1.26.0 make verify-api

# --- VPN + lab ---
scutil --nc start "F5 Access"
# wait until: scutil --nc status "F5 Access" | head -1  → Connected

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=25 -o IdentityAgent=none
  -o IdentitiesOnly=yes -o IdentityFile="$HOME/.ssh/<lab-ssh-key>"
  -o UseKeychain=yes -o KexAlgorithms=curve25519-sha256)

# inventory (read-only)
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> '
k3s kubectl version --short
for c in certificates.cert-manager.io tenantcontrolplanes.kamaji.clastix.io \
         applications.argoproj.io dpus.provisioning.dpu.nvidia.com; do
  k3s kubectl get crd "$c" >/dev/null 2>&1 && echo PRESENT "$c" || echo ABSENT "$c"
done
k3s kubectl get deploy -A | grep -E "cert-manager|kamaji|argo|dpf-operator"
k3s kubectl get dpucluster -A
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'

# build linux/amd64 image and import (job dir deleted after)
docker build --platform linux/amd64 -t controller:latest .
docker save controller:latest | gzip > /tmp/controller-deps.tar.gz
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'mkdir -p /tmp/lab-job-deps'
scp "${SSH_OPTS[@]}" /tmp/controller-deps.tar.gz \
  root@<your-lab-hosts>:/tmp/lab-job-deps/controller.tar.gz
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'set -e
gzip -dc /tmp/lab-job-deps/controller.tar.gz | k3s ctr images import -
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout restart \
  deploy/opi-nvidia-dpf-adapter-controller-manager
k3s kubectl -n opi-nvidia-dpf-adapter-system rollout status \
  deploy/opi-nvidia-dpf-adapter-controller-manager --timeout=180s
rm -rf /tmp/lab-job-deps
'

# after deploy: must be Blocked, not Supported
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> '
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason} {.message}{\"\\n\"}{end}"
echo "---"
k3s kubectl -n opi-nvidia-dpf-adapter-system logs \
  deploy/opi-nvidia-dpf-adapter-controller-manager --tail=40 | grep "ensure installed"
'

# AMD must not require Argo (delete after)
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl apply -f - <<EOF
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata:
  name: lcm-amd-preflight
spec:
  vendor: amd
  logLevel: 2
EOF
sleep 4
k3s kubectl get dpuoperatorconfig lcm-amd-preflight \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
k3s kubectl delete dpuoperatorconfig lcm-amd-preflight
'
```

Need: F5 VPN, `<lab-ssh-key>`, Go 1.26, Docker for linux/amd64.
Do not helm-install Argo or Kamaji to force `Supported`.
Do not helm-uninstall DPF. Do not apply AMD stub CRDs on dh2.
