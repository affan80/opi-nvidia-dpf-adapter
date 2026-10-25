# Pipeline lab fixes

Date: 2026-10-23
Lab: dh2 (`<your-lab-hosts>`), k3s `v1.36.4+k3s1`, DPF `v26.4.0`, adapter
`opi-nvidia-dpf-adapter` on branch `feat/schema-api-lab-fixes` (uncommitted
mapping/LCM work still local).

This folder is the report for: fix each pipeline issue one by one, retest
after each fix, then retest the whole pipeline, and record every issue,
where it lived, every pipeline-level vulnerability, what changed, and how.

| File | What |
|---|---|
| [01_lab_connection.md](01_lab_connection.md) | VPN + SSH hop onto dh2 |
| [02_issues.md](02_issues.md) | Ordered issues, where they were, before/after |
| [03_vulnerabilities.md](03_vulnerabilities.md) | Pipeline-level vulns found and how they were handled |
| [04_fixes.md](04_fixes.md) | What was fixed and how, in detail |
| [05_commands.md](05_commands.md) | Commands run locally and on the lab |
| [06_results.md](06_results.md) | Final combined pipeline retest |
| [07_remaining.md](07_remaining.md) | What is still blocked and why it was not forced |

Second pass (same day): remaining items were fixed. Kamaji stayed
uninstalled; a **static** `DPUCluster` uses an in-cluster ServiceAccount
kubeconfig against this k3s API. DPF then allocated the DPU and started
**OS Installing** (real BFB flash). Argo Applications appeared on their
own.

Constraints that stayed in force: do not helm-uninstall DPF, do not apply
AMD stubs on BlueField, do not copy `<lab-ssh-key>`, do not change `/etc`,
packages, `systemctl`, firmware, BMC, or reboot. Job files on dh2 lived
in `/tmp/lab-job-pipeline-fix/` and were deleted after the retest.
