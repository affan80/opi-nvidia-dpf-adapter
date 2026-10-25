# Commands

## VPN and lab

```bash
scutil --nc start "F5 Access"
scutil --nc status "F5 Access"
ping -c 2 <your-lab-hosts>
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@<your-lab-hosts> \
  'hostname; k3s --version'
```

## Unit tests

```bash
cd /Users/kartikey0104/Desktop/opi-dpf-adapter/opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestDecide|TestValidateArtifact|TestSanitize|TestSupportedUpgrade|TestSameVersion|TestUnsupported|TestDowngrade|TestConcurrent|TestUpgradeFailure|TestTimeout|TestUnknown|TestRetry|TestSuccessfulRollback|TestFailedRollback|TestHelmClientDoesNotLog'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

## Lab inventory (read-only)

Do **not** `helm upgrade`, `helm uninstall`, or stamp
`lifecycle.opi.nvidia.com/owner` on the live release.

```bash
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@<your-lab-hosts> '
k3s kubectl get secret -n dpf-operator-system -l owner=helm \
  -o jsonpath="{range .items[*]}{.metadata.name} owner={.metadata.labels.owner}{\"\\n\"}{end}"
k3s kubectl get deploy -n dpf-operator-system dpf-operator-controller-manager \
  -o jsonpath="{.spec.template.spec.containers[0].image}{\"\\n\"}"
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

Job files, if any, go in `/tmp/lab-job-upgrade-policy/` and are
deleted afterward.
