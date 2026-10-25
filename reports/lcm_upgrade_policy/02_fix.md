# Fix

Files:

- `pkg/lifecycle/version_policy.go` — decide
- `pkg/lifecycle/helm_client.go` — apply
- `pkg/lifecycle/installer.go` — `InstallResult` fields
  (`Observed`, `LastKnownGood`, `OpStatus`, `Failure`) plus actions
  `noop|upgraded|rolled_back|failed`

## Version policy

`ValidateReleaseVersion` rejects empty, URL, shell metacharacters,
and unparseable strings (`latest`).

`ValidateDesiredVersion` requires the NVIDIA desired version to be
both in `[v26.4.0, v27.0.0)` and equal to `NVIDIAChartVersion`.
`v26.4.2` is in the window and still rejected as desired because it
is not the pin.

`ValidateArtifactRef` re-checks the install allow-list: HTTPS NGC
repo, exact pin, empty `HelmArgs`. `file://` and `latest` fail.

`DecideReleaseAction(installed, desired)`:

| Installed | Desired | AllowDowngrade | Action |
|---|---|---|---|
| `v26.3.9` | `v26.4.0` | false | upgrade |
| `v26.4.0` | `v26.4.0` | false | noop |
| `v26.4.2` | `v26.4.0` | false | reject (`ErrDowngradeForbidden`) |
| `v26.4.2` | `v26.4.0` | true | downgrade (still Upgrade onto the pin) |
| `latest` / URL / `--set` | pin | any | reject (`ErrMalformedVersion`) |
| any | `v27.1.0` | any | reject (`ErrUnsupportedVersion`) |

## Helm apply

`ReleaseLocks` holds one mutex per `namespace/name`. Upgrade and
rollback run under that lock.

Before Upgrade, `RememberLastKnownGood` stores the current owned
release.

`upgradeOrRollback`:

1. Call Upgrade (max 2 attempts for transient errors).
2. Success only if the returned release is the pin and `deployed`.
3. On error, `GetRelease` first. If the cluster already shows the
   pin + `deployed`, report success (the write landed).
4. `ErrReleaseUnknown` is recorded as `unknown` and is **not**
   retried.
5. Timeout / cancel reconciles actual state and reports `timeout`,
   never `succeeded`, unless GetRelease proves the pin is deployed.
6. After the attempt budget, `Rollback` to last-known-good.
7. Rollback success requires version + `deployed`. Otherwise
   `OpRollbackFailed` and `ErrRollbackFailed`.

`SanitizeLogValue` redacts password, token, secret, credential,
kubeconfig, authorization, Bearer, and PEM-like values.

`cmd/main.go` still leaves `LifecycleManager.Helm` nil so the
unmanaged lab DPF is not upgraded.
