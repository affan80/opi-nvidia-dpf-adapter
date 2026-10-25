# Issue

The LCM needed least-privilege RBAC, tests for install/upgrade/cleanup
failure paths, and supply-chain checks before it could be called
stable.

Risks if that is missing:

- ClusterRole wildcards or delete on CRDs / DpuOperatorConfig
- Helm upgrade or uninstall of the live unmanaged `dpf-operator`
- Cleanup deleting shared CRDs or another tenant's dataplane
- CI using unpinned `kind` latest and no `go mod verify`
- Silent success after timeout, unknown Helm outcome, or failed rollback
