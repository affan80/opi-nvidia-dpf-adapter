# Commands

```bash
scutil --nc start "F5 Access"
cd /Users/kartikey0104/Desktop/opi-dpf-adapter/opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestControllerOwned|TestAdoptedRelease|TestExternallyOwned|TestMissingOrConflicting|TestSharedCRDs|TestSharedDataplane|TestNamespaceScoped|TestOwnershipChange|TestCleanupIsIdempotent|TestCleanupFailures|TestCleanupReconcile|TestHelmUninstallRefuses'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

Lab (read-only — do **not** helm-uninstall):

```bash
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/opi_lab_key" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@172.22.1.2 '
k3s kubectl get secret -n dpf-operator-system \
  sh.helm.release.v1.dpf-operator.v1 \
  -o jsonpath="owner={.metadata.labels.owner}{\"\\n\"}"
'
```
