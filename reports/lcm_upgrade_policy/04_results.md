# Results

Date: 2026-10-24. VPN Connected. SSH to dh2 OK.

## Unit tests on dh2

Compiled on this Mac (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`,
Go 1.26.0) and executed on dh2 under `/tmp/lab-job-upgrade-policy/`.
dh2 has no Go toolchain (`go` is not on PATH; `k3s --version` only
prints the k3s build Go `1.26.7`). Job dir deleted afterward.

| Test | Result |
|---|---|
| TestSupportedUpgradeSucceeds | PASS — `v26.3.9` → `v26.4.0`, last-known-good kept |
| TestSameVersionIsNoop | PASS |
| TestUnsupportedAndMalformedVersionsRejected | PASS — `latest`, `v27.1.0` |
| TestDowngradeRejectedUnlessAllowed | PASS |
| TestConcurrentOperationsAreSerialized | PASS — `MaxInflight == 1` |
| TestUpgradeFailurePreservesLastKnownGood | PASS |
| TestTimeoutReconcilesActualState | PASS — `OpTimeout`, not upgraded |
| TestUnknownOutcomeIsNotRetried | PASS — one Upgrade, `OpUnknown` |
| TestRetrySucceedsWithoutDuplicateRelease | PASS — two Upgrades, one release |
| TestSuccessfulRollbackRestoresExpectedRelease | PASS — restored `v26.3.9` |
| TestFailedRollbackIsNotSuccess | PASS — `OpRollbackFailed` |
| TestHelmClientDoesNotLogSecretsOrShellOut | PASS |
| TestDecideReleaseActionUpgradeNoopDowngrade | PASS |
| TestDecideReleaseActionRejectsMalformedAndUnsupported | PASS |
| TestValidateArtifactRefPinnedAndImmutable | PASS |
| TestSanitizeLogValueRedactsSecrets | PASS |
| Full `pkg/lifecycle` package | PASS |

Local `GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1`
also **ok** (0.577s). `golangci-lint run ./pkg/lifecycle/` → 0 issues.

## Lab inventory (read-only, no helm-upgrade)

| Item | Value |
|---|---|
| k3s | `v1.36.4+k3s1` |
| DPF image | `nvcr.io/nvidia/doca/dpf-system:v26.4.0` |
| Helm secret `dpf-operator` | `owner=helm` (no adapter owner) |
| Helm CLI | absent |
| LCM | `VendorOperatorSupported=True/Supported`, `VendorOperatorReady=True/Ready` |
| Adapter manager | `opi-nvidia-dpf-adapter-controller-manager-546d6dcbc9-mgrcg` Running |
| Helm in `cmd/main.go` | still nil — unmanaged lab DPF was not adopted or upgraded |

## Security gates

- Unsupported or malformed desired versions cannot become the chart pin.
- Concurrent Ensures share one mutex.
- Success requires the pin and status `deployed`. Timeout, unknown,
  and rollback failure have their own `OpStatus` values and are not
  reported as succeeded.
