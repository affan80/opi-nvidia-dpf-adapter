# Commands

## VPN and lab

```bash
scutil --nc start "F5 Access"
ping -c 3 <your-lab-hosts>
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@<your-lab-hosts> \
  'hostname; k3s --version'
```

## Local

```bash
cd /Users/kartikey0104/Desktop/opi-dpf-adapter/opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ ./pkg/mapping/ -count=1
GOTOOLCHAIN=go1.26.0 go test -race ./pkg/lifecycle/ -count=1
GOTOOLCHAIN=go1.26.0 go vet ./pkg/lifecycle/
GOTOOLCHAIN=go1.26.0 go mod tidy
GOTOOLCHAIN=go1.26.0 go mod verify
make govulncheck
```

## Lab (linux/amd64 binary, job dir deleted after)

Do **not** helm-upgrade or helm-uninstall.

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=go1.26.0 \
  go test -c -o /tmp/lifecycle.test ./pkg/lifecycle/
# rsync sources + binary to /tmp/lab-job-lcm-sec/ on dh2
# run lifecycle.test from src/pkg/lifecycle
k3s kubectl get secret -n dpf-operator-system -l owner=helm
k3s kubectl get clusterrole opi-nvidia-dpf-adapter-manager-role -o yaml
```
