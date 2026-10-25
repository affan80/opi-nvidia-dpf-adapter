# Fix

New `pkg/lifecycle/status.go` plus `ClusterProbe.Deployment`.

| Phase | Meaning |
|---|---|
| Installing | required CRD or Deployment is missing |
| Degraded | objects exist but a controller/prereq is not healthy |
| Ready | CRDs + DPF controllers + cert-manager + Argo server are healthy |

`AssessReadiness.Ready` is true only for Ready.
LCM writes `VendorOperatorReady` (not `Ready`).

On dh2: patched ClusterRole for `apps/deployments` get/list/watch.
Rebuilt `controller:latest` linux/amd64, imported, restarted manager.
No new packages. No Helm install. No AMD stubs. Prereqs were already present.
