# Commands

```bash
# local
cd opi-nvidia-dpf-adapter
go test ./pkg/lifecycle/ ./pkg/mapping/ ./pkg/translation/ -count=1
GOTOOLCHAIN=go1.26.0 make lint

# lab VPN
scutil --nc start "F5 Access"
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=25 -o IdentityAgent=none
  -o IdentitiesOnly=yes -o IdentityFile="$HOME/.ssh/opi_lab_key"
  -o UseKeychain=yes -o KexAlgorithms=curve25519-sha256)

# InvalidConfig probe (delete after)
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'k3s kubectl apply -f - <<EOF
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata:
  name: lcm-evil-helm
  annotations:
    lifecycle.opi.nvidia.com/vendor: nvidia
    lifecycle.opi.nvidia.com/helm-args: "--set image=evil"
spec:
  logLevel: 2
EOF
sleep 3
k3s kubectl get dpuoperatorconfig lcm-evil-helm -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
k3s kubectl -n opi-nvidia-dpf-adapter-system logs deploy/opi-nvidia-dpf-adapter-controller-manager --tail=40 | grep -E "InvalidConfig|ensure installed"
k3s kubectl delete dpuoperatorconfig lcm-evil-helm
'
```

Need: F5 VPN, `opi_lab_key`, Go 1.26, DPF already installed. Do not helm-uninstall DPF. Do not apply AMD stubs on dh2.
