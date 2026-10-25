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

## Translation gate

DPF CRDs are created while the LCM Helm install is still running. A
source created in that window is not applied and is not dropped.

`pkg/translation` waits when a `DpuOperatorConfig` exists and none of
them has `VendorOperatorReady` status `True` (`Installing` or
`Degraded`). It also waits when discovery does not yet serve an emitted
GVK. The wait is `RequeueAfter` 15s with a nil error, so the work queue
does not spin. A `VendorOperatorReady=True` transition, or a served
target CRD, enqueues the source immediately. A stale REST mapper is
reset once per attempt. Kinds discovery does not serve are not
`Owns()`'d, so a missing CRD does not fail manager cache sync. A
mapping that emits nothing (for example a non-BlueField source on the
NVIDIA mapping) is not requeued.

## Commands

```bash
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestObserveOperator|TestAssessReadiness|TestEnsureInstalledReadyOnlyWhenHealthy|TestReconcileWritesVendorOperatorReady'
```
