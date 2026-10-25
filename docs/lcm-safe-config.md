# Safe, validated LCM install settings

## Issue

`EnsureInstalled` classified vendor/version but had no pinned Helm
settings. A later installer could have taken a chart URL, `--set`, or
namespace from `DpuOperatorConfig` annotations. That is arbitrary Helm
execution.

## Approach

`pkg/lifecycle/config.go` is the only source of Helm settings.
`version_policy.go` keeps the chart pin even when the cluster version
annotation is empty or out of window.

| Field | Pinned value |
|---|---|
| vendor | `nvidia` (amd: known, no chart) |
| namespace | `dpf-operator-system` |
| chart | `dpf-operator` |
| repo | `https://helm.ngc.nvidia.com/nvidia/doca` |
| version | `v26.4.0` |
| release | `dpf-operator` |
| owner | `opi-nvidia-dpf-adapter` |
| helm args | empty (forbidden) |

Rejected (condition `VendorOperatorSupported=False` reason `InvalidConfig`):
chart-url, chart-repo, helm-args, helm-set, release-namespace, a version
pin that is a URL. Values are not copied into `InstallConfig`.

Same vendor always yields the same `DefaultInstallConfig` (deterministic).

## Commands

```bash
go test ./pkg/lifecycle/ -count=1 -run 'TestDefaultInstallConfig|TestValidateInstallConfig|TestReject|TestApplyVersionPolicy|TestEnsureInstalledRejectsHelmOverride|TestInstallConfigMatches'
GOTOOLCHAIN=go1.26.0 make lint
```

Lab (temp object, then delete):

```bash
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
k3s kubectl delete dpuoperatorconfig lcm-evil-helm
'
```

Need: pinned `config/versions.yaml` helm block, DPF already on the
cluster, no helm-uninstall.
