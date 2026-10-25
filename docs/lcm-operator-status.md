# Observe CRD and controller readiness

## Issue

After DPF was installed, `AssessReadiness.Ready` became true as soon as
the `DPU` REST mapping existed. That is not health. A missing
`dpf-operator-controller-manager` replica or a half-applied CRD set
still looked like success.

The daemon owns condition type `Ready` on `DpuOperatorConfig`. The LCM
must not write that type.

## Fix

`pkg/lifecycle/status.go` observes the cluster after install.

| Outcome | When |
|---|---|
| `Installing` | a required CRD or Deployment is absent |
| `Degraded` | every required object exists, but a controller or prereq is not healthy |
| `Ready` | required DPF CRDs, DPF controller Deployments, and prereq Deployments all exist and report healthy |

`AssessReadiness.Ready` is true only for that last row. The LCM publishes
`VendorOperatorReady` (reasons Installing / Ready / Degraded). It still
does not write `Ready`.

Required NVIDIA look-up:

- CRDs: `DPU`, `DPUDevice`, `BFB`, `DPUService`, `DPFOperatorConfig`
- Controllers: `dpf-operator-controller-manager`,
  `dpf-provisioning-controller-manager`, `dpuservice-controller-manager`
- Prerequisites: `cert-manager/cert-manager`,
  `dpf-operator-system/argo-cd-argocd-server` (or `argocd/argocd-server`)

AMD has no Helm chart. Ready only if `DSCDevice` exists. Do not apply
AMD stubs on a BlueField host.

## Commands

```bash
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestObserveOperator|TestAssessReadiness|TestEnsureInstalledReadyOnlyWhenHealthy|TestReconcileWritesVendorOperatorReady'
```
