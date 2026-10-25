# Commands

Cwd: `opi-nvidia-dpf-adapter/`.

## Local

```bash
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestNVIDIAOLM|TestVersionsYAMLPinsOLM|TestRejectOLM|TestClusterOLM|TestOLMInstaller|TestEnsureInstalledOLM|TestEnsureInstalledHelmWins|TestMainWiresOLM|TestOLMClientSource|TestManagerRoleObservesOLM'
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ ./pkg/mapping/ -count=1
GOTOOLCHAIN=go1.26.10 go test -race ./pkg/lifecycle/ -count=1
make verify-manifests
make test-e2e
```

## Lab

```bash
scutil --nc start "F5 Access"
SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=25
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
)
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> '
k3s kubectl get crd subscriptions.operators.coreos.com \
  clusterserviceversions.operators.coreos.com
k3s kubectl get subscription,csv -A
k3s kubectl get secret -n dpf-operator-system -l owner=helm
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

Compiled `GOOS=linux GOARCH=amd64` `lifecycle.test`, copied with
source files into `/tmp/lab-job-olm/`, ran the OLM tests on dh2,
deleted the job dir. Did not install OLM. Did not helm-uninstall.
