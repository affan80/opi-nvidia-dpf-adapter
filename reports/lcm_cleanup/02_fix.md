# Fix

`pkg/lifecycle/cleanup.go` decides. `lifecycle_controller.go` runs
it when the `lifecycle.opi.nvidia.com/cleanup` finalizer holds a
deleting `DpuOperatorConfig`.

Ownership:

| Class | Metadata | Delete release? |
|---|---|---|
| controller | owner=adapter and mode=installed | only if `removal-policy=delete-owned` |
| adopted | owner=adapter, mode adopted/empty | only if `adoption-policy=delete-owned` |
| external | `owner=helm` or any other owner | never |
| invalid | missing labels, or adapter owner plus `owner=helm` | never (fail safe) |

Default policy is `retain`. No Helm client → ownership cannot be
verified → preserve. GetRelease is called again immediately before
Uninstall. Shared CRDs stay unless `AllowCRDDeletion` (production
false) and no other tenants/OPI/dataplane remain. Dataplane deletes
are `namespace/kind/name` and skip referenced or foreign-namespace
objects.

`VendorOperatorCleanup` records the outcome. Logs go through
`SanitizeLogValue`. `cmd/main.go` Helm stays nil.
