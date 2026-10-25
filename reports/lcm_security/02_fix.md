# Fix

## RBAC

Kubebuilder markers on `lifecycle_controller.go` (then `make manifests`):

- `dpuoperatorconfigs`: get/list/watch/**update** (finalizer)
- `dpuoperatorconfigs/finalizers`: update
- `dpuoperatorconfigs/status`: get/update/patch
- `deployments`: get/list/watch
- `dpuclusters`, `dpfoperatorconfigs`: get/list/watch only
- CRDs: get/list (no delete)

`make verify-manifests` diffs `config/rbac/role.yaml` against a fresh
controller-gen output. Tests reject `*` and delete on shared
control-plane objects.

## Lifecycle behavior (already in tree, re-tested)

- Pinned Helm install/adopt; unmanaged `owner=helm` is not adopted
- Upgrade only onto `v26.4.0`; same version is no-op; downgrade
  rejected unless allowed
- Desired NVIDIA version must be the **exact** pin string
- Timeouts/unknown outcomes reconcile `GetRelease` and are not
  reported as success
- Cleanup classifies controller / adopted / external / invalid
- Default removal policy is retain; invalid policy retains
- Shared CRDs and referenced dataplane stay

## CI

- Tests: `go mod tidy` (fail on drift), `go mod verify`, `make test`,
  `go test -race ./pkg/lifecycle/`
- Security workflow: `make govulncheck`
- E2E: Kind **v0.32.0**, not `latest`
- Dependabot for gomod and github-actions
- Workflows: `permissions: {}` plus job `contents: read`

## Not done on purpose

- `helm.sh/helm/v3` not added (fights controller-runtime / k8s v0.36)
- No helm-uninstall of live DPF
- `cmd/main.go` Helm stays nil
- Live ClusterRole **not** patched this run (compare only)
- No blind `x/net` v0.60 or grpc bump
