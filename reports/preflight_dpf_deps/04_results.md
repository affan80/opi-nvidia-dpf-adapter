# Lab results 2026-10-23

## Before (old image, no dependency gate)

```
VendorOperatorSupported=True/Supported
  nvidia operator v26.4.0 is within the supported range [v26.4.0, v27.0.0)

ensure installed vendor=nvidia source=spec action=detected
  installed=v26.4.0 skew=Supported ready=true
```

Argo CD was already absent. That is a false success.

## After (`dependency_checker.go` on dh2)

```
VendorOperatorSupported=False/Blocked
  DPF v26.4.0 is blocked: missing argo-cd

ensure installed vendor=nvidia source=spec action=skipped
  installed=v26.4.0 skew=Blocked ready=false
  kubernetes=v1.36.4+k3s1 missing=argo-cd
```

| Check | Got | Want |
|---|---|---|
| NVIDIA condition | False / Blocked | False / Blocked |
| NVIDIA message | missing argo-cd | names the missing dep |
| NVIDIA installer action | skipped | skipped (not detected) |
| AssessReadiness | ready=false | false |
| Kubernetes seen | v1.36.4+k3s1 | supported minor |
| Kamaji | not required (no DPUCluster) | not in missing |
| LCM wrote Ready | no | no |
| AMD temp object | Unknown / Unpinned, action=absent | not Blocked on Argo |
| AMD DSC CRD | absent | absent |
| Helm of DPF / Argo | not run | not run |

AMD object `lcm-amd-preflight` was deleted after the probe.

## Local tests

```
go test ./pkg/lifecycle/ ./pkg/mapping/ ./pkg/translation/ -count=1
ok
make lint
0 issues
make verify-api
ok
```
