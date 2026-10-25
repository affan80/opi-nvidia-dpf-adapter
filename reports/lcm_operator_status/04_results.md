# Results

## Unit

```
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
ok
```

Installing / Degraded cases are not Ready. Healthy observation is Ready.

## Lab dh2 (2026-10-23 20:33 UTC)

Prereqs already installed (nothing new):

| Resource | Result |
|---|---|
| k3s | `v1.36.4+k3s1` |
| cert-manager | `v1.21.1` 1/1 |
| Argo CD server | `v3.4.6` 1/1 |
| DPU / DPUDevice / BFB / DPUService / DPFOperatorConfig CRDs | present |
| AMD `DSCDevice` | absent (correct on BlueField) |
| dpf-operator-controller-manager | 1/1 |
| dpf-provisioning-controller-manager | 2/2 |
| dpuservice-controller-manager | 1/1 |

After image import:

```
VendorOperatorSupported=True/Supported nvidia operator v26.4.0 ...
VendorOperatorReady=True/Ready DPF CRDs, controllers, and prerequisites are healthy
```

Log: `action=detected installed=v26.4.0 skew=Supported ready=true operator=Ready kubernetes=v1.36.4+k3s1`
