# Pinned upgrades and rollback policy

## Issue

Install/adopt could converge on one release, but a later reconcile
could still:

- accept `latest`, a chart URL, or a version outside `[v26.4.0, v27.0.0)`
- treat a same-version request as an upgrade
- downgrade a newer owned release onto the pin without an explicit flag
- run two upgrades of `dpf-operator` at once
- report success after a timeout without reading the actual release
- retry an operation whose outcome is unknown
- lose the last-known-good version after a failed upgrade
- report rollback as success when Helm did not restore that version

The live lab `dpf-operator` Helm secret is still `owner=helm`. This
policy applies only to adapter-owned releases. `cmd/main.go` wires
`SecretHelmClient`, which reports that release as `unmanaged` and
does not upgrade or uninstall it.

## Fix

`pkg/lifecycle/version_policy.go` decides; `pkg/lifecycle/helm_client.go`
applies the decision under a per-release lock.

| Gate | Rule |
|---|---|
| Desired version | Must parse, sit in the vendor window, and equal `NVIDIAChartVersion` (`v26.4.0`) |
| Installed version | Must parse. `latest`, URLs, and `--` flags are malformed |
| Artifact | HTTPS allow-list repo, exact pin, no `HelmArgs`, no floating tag |
| Same version | `noop` — no Upgrade call |
| Installed older than pin | `upgrade` |
| Installed newer than pin | reject unless `AllowDowngrade` |
| Concurrent Ensure | one mutex per `namespace/name` |
| Before Upgrade | store last-known-good |
| After Upgrade / timeout | `GetRelease`; success only if version is the pin and status is `deployed` |
| Unknown outcome | record `unknown`, do not retry |
| Transient failure | at most two attempts, then rollback |
| Rollback | success only after `GetRelease` shows last-known-good + `deployed`; else `rollback_failed` |

`InstallResult` now records `Observed`, `LastKnownGood`, `OpStatus`,
and `Failure`. `OpTimeout`, `OpUnknown`, and `OpRollbackFailed` are
never rewritten as `succeeded`.

`SanitizeLogValue` redacts token / password / kubeconfig /
authorization values. Helm client code does not call `exec.Command`
and does not build `helm --set` strings.

AMD still has no chart. Helm/OLM return `ErrNoVendorChart` and
DetectingInstaller reports `absent` or `detected` from `DSCDevice`.

## Commands and versions

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestDecide|TestValidateArtifact|TestSanitize|TestSupportedUpgrade|TestSameVersion|TestUnsupported|TestDowngrade|TestConcurrent|TestUpgradeFailure|TestTimeout|TestUnknown|TestRetry|TestSuccessfulRollback|TestFailedRollback|TestHelmClientDoesNotLog'
GOTOOLCHAIN=go1.26.0 go test ./pkg/lifecycle/ -count=1
```

Lab (read-only — do **not** helm-upgrade):

```bash
scutil --nc start "F5 Access"
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/opi_lab_key" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 root@172.22.1.2 '
k3s --version
k3s kubectl get secret -n dpf-operator-system -l owner=helm \
  -o jsonpath="{range .items[*]}{.metadata.name} owner={.metadata.labels.owner}{\"\\n\"}{end}"
k3s kubectl get deploy -n dpf-operator-system dpf-operator-controller-manager \
  -o jsonpath="{.spec.template.spec.containers[0].image}{\"\\n\"}"
k3s kubectl get dpuoperatorconfig dpu-operator-config \
  -o jsonpath="{range .status.conditions[*]}{.type}={.status}/{.reason}{\"\\n\"}{end}"
'
```

| Tool / pin | Version |
|---|---|
| Go | 1.26.0 |
| Kubernetes modules | v0.36.0 |
| DPF chart pin | `dpf-operator` `v26.4.0` |
| Chart repo | `https://helm.ngc.nvidia.com/nvidia/doca` |
| Window | `[v26.4.0, v27.0.0)` |
| Owner label | `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter` |

## Unit results

```
TestDecideReleaseActionUpgradeNoopDowngrade           PASS
TestDecideReleaseActionRejectsMalformedAndUnsupported PASS
TestValidateArtifactRefPinnedAndImmutable             PASS
TestSanitizeLogValueRedactsSecrets                    PASS
TestSupportedUpgradeSucceeds                          PASS
TestSameVersionIsNoop                                 PASS
TestUnsupportedAndMalformedVersionsRejected           PASS
TestDowngradeRejectedUnlessAllowed                    PASS
TestConcurrentOperationsAreSerialized                 PASS
TestUpgradeFailurePreservesLastKnownGood              PASS
TestTimeoutReconcilesActualState                      PASS
TestUnknownOutcomeIsNotRetried                        PASS
TestRetrySucceedsWithoutDuplicateRelease              PASS
TestSuccessfulRollbackRestoresExpectedRelease         PASS
TestFailedRollbackIsNotSuccess                        PASS
TestHelmClientDoesNotLogSecretsOrShellOut             PASS
go test ./pkg/lifecycle/ -count=1                     ok
```

## Lab result (2026-10-24)

VPN Connected. SSH to dh2 OK. k3s `v1.36.4+k3s1`. DPF image
`nvcr.io/nvidia/doca/dpf-system:v26.4.0`. Helm secret
`sh.helm.release.v1.dpf-operator.v1` is still `owner=helm` —
unmanaged; no helm-upgrade was issued. LCM
`VendorOperatorSupported=True/Supported` and
`VendorOperatorReady=True/Ready`. DPU `dh2-bluefield-2` phase
`Rebooting` (unchanged; no firmware reset).

Lifecycle tests compiled `linux/amd64` and ran on dh2 under
`/tmp/lab-job-upgrade-policy/` (deleted after). All named
upgrade/rollback cases **PASS**. Full package **PASS**.
