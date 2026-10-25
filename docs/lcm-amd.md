# Finish the AMD LCM path without inventing a chart

## Issue

`spec.vendor` allows `amd`, but AMD Pensando has **no published DSC
operator Helm chart**. HelmInstaller treated that as a successful
`skipped` and never ran DetectingInstaller. With Helm wired in
`cmd/main.go`, a real DSC API would stay `Present=false`.

Latest public AMD Helm is not a DSC DPU operator:

- AMD Network Operator — Pollara AI NIC, not DSC
- Pensando Lago — telemetry microservices

Done-when: AMD never writes a chart or Subscription; BlueField reports
`absent`; a present `DSCDevice` API is `detected`; stubs stay off this
lab.

## Fix

| Piece | Role |
|---|---|
| `ErrNoVendorChart` | Helm/OLM AMD skip is a fallback, not a finished install |
| `helmWithDetect` / `olmWithDetect` | fall through to DetectingInstaller |
| `config/versions.yaml` `amd:` | `helm.published: false`, `olm.published: false`, empty chart/package |
| DetectingInstaller | REST-maps `dpu.amd.com/DSCDevice` only |

Do not pin `network-operator` or `pensando/lago`. Do not apply
`test/conformance/crds/amd` on a BlueField host.

## Commands

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestVersionsYAMLPinsAMD|TestEnsureInstalledAMD|TestHelmWithDetectAMD|TestHelmInstallerAMD|TestOLMInstallerAMD|TestDetectingInstallerAMD|TestAMDHasNoHelm|TestObserveOperatorAMD|TestCheckDependenciesAMD'
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1
GOTOOLCHAIN=go1.26.10 go test -race ./pkg/lifecycle/ -count=1
```

Lab (inventory only — do **not** apply AMD stub CRDs):

```bash
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/<ssh-key>" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@<lab-host> '
k3s kubectl get crd dscdevices.dpu.amd.com
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{.spec.vendor}{\"\\n\"}{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

Want: `dscdevices.dpu.amd.com` **NotFound**, live vendor still `nvidia`.

## Lab result (2026-10-24)

VPN Connected, SSH to dh2 OK. k3s `v1.36.4+k3s1`.

```
dscdevices.dpu.amd.com     NotFound
dscprofiles.dpu.amd.com    NotFound
spec.vendor                nvidia
VendorOperatorSupported    True/Supported
VendorOperatorReady        True/Ready
Helm dpf-operator          owner=helm
```

Compiled `GOOS=linux GOARCH=amd64` `lifecycle.test`. AMD-focused tests
**PASS**. After the run: still no AMD CRDs, live vendor still `nvidia`,
job dir deleted. Stubs were not applied.

Kind e2e: **5/5 PASS**, cluster deleted.
