# How each Blocked case is fixed

`Blocked` is the correct LCM result. The fix is on the cluster, not a
code path that pretends the prerequisite is present.

| Case | LCM result | Fix |
|---|---|---|
| Missing Argo CD | `False/Blocked`, installer skipped | Install Argo CD (`applications.argoproj.io`). Do not install it on dh2 from this adapter. |
| Missing cert-manager | `False/Blocked`, installer skipped | Install cert-manager. dh2 already has it. |
| Kubernetes v1.29 | `False/Blocked`, names v1.29 and v1.36 | Upgrade to Kubernetes 1.36.x. dh2 is `v1.36.4+k3s1`. |
| Unreachable API | `False/Blocked` `cannot reach the Kubernetes API` | Restore apiserver / credentials. |
| Nil probe | `False/Blocked` `no cluster probe` | `cmd/main.go` must keep the discovery client wired. |
| Missing prerequisite looking like success | was the bug | `CheckDependencies` runs before the installer; `action=skipped`. |

Unit proof: `TestEnsureInstalledBlockedCases`,
`TestReconcilePublishesBlockedCondition`.
Lab proof on dh2: NVIDIA `missing argo-cd` → `False/Blocked`.
