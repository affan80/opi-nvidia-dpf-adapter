# Lifecycle Manager — OPI test report

**Date:** 2026-10-25  
**Repo:** `opi-nvidia-dpf-adapter` (`github.com/kartikeyg0104/opi-nvidia-dpf-adapter`)  
**Lab:** dh2 `<your-lab-hosts>` (BlueField-2, k3s `v1.36.4+k3s1`, DPF Helm `v26.4.0`)  
**VPN Mac:** `<vpn-mac-host>.local` (`<your-lab-hosts>`), user `kartikey0104`  
**Image on lab (last roll):** `controller:lcm-20261024b`  
**Pin stays:** NVIDIA DPF **v26.4.0** (v26.4.1 reviewed, not taken; BF3-tested patch, lab is BF2)

This folder is the roll-up of every Lifecycle Manager (LCM) fix, test,
security finding, and e2e run from the Hybrid work. It sits next to the
earlier topic folders (`lcm_helm_client`, `lcm_security`, `lcm_cleanup`,
…). Those stay as the per-task notes. **This folder is the full LCM
story.**

## Documents

| File | What |
|---|---|
| [01_lcm_fix_build_tests.md](01_lcm_fix_build_tests.md) / [01_lcm_fix_build_tests.pdf](01_lcm_fix_build_tests.pdf) | What we built, how to build, every command, every test |
| [02_security_cves.md](02_security_cves.md) / [02_security_cves.pdf](02_security_cves.pdf) | Threats, CVEs (GO-2026-*), how each was patched |
| [03_issues_and_fixes.md](03_issues_and_fixes.md) / [03_issues_and_fixes.pdf](03_issues_and_fixes.pdf) | Each LCM issue, where it was, how it was fixed |
| [04_e2e_report.md](04_e2e_report.md) / [04_e2e_report.pdf](04_e2e_report.pdf) | Kind e2e (6/6) and live dh2 user-flow in depth |
| [05_lab_commands_from_start.pdf](05_lab_commands_from_start.pdf) | Commands to retest LCM from start on dh2 |
| [06_e2e_lab_run.pdf](06_e2e_lab_run.pdf) | **This run (2026-10-25 04:51Z):** 188 PASS on dh2, live user-flow stall at DPU Rebooting |

## What LCM is

The LCM watches cluster-scoped `DpuOperatorConfig` (`config.openshift.io/v1`)
and runs `EnsureInstalled`: intent → preflight → safe config →
dependencies → installer (Helm observe/claim → OLM observe → detect) →
version policy → operator readiness → NVIDIA DPU observe.

It does **not** write daemon `Ready`. It writes:

- `VendorOperatorSupported`
- `VendorOperatorReady`
- `VendorOperatorCleanup`
- `VendorDPUReady` (NVIDIA DPU Rebooting / host-power-cycle; observe only)

## Short answer

| Area | State |
|---|---|
| Unit `./pkg/lifecycle` | PASS (this Mac; also compiled-lab PASS on dh2) |
| Race | PASS |
| `make govulncheck` | exit **0**, 0 call-graph hits (was 43) |
| Kind e2e | **6/6 PASS** after Docker preflight (HelmProbe is the 6th) |
| Live DPF Helm | claimed pin `dpf-operator-system/dpf-operator@v26.4.0`, `action=noop` |
| Live DPU | still **Rebooting** — no `mlxfwreset`, no host reboot |
| Production NGC Helm install/upgrade of live `dpf-operator` | still refused (`ClusterHelmClient`) |
| AMD DSC chart | unpublished; detect fallthrough; stubs not applied on BlueField |

## Constraints that stayed in force

- Do not helm-uninstall live DPF
- Do not run `mlxfwreset` or reboot the host
- Do not apply AMD DSC stubs on BlueField
- Do not copy `<lab-ssh-key>`
- Do not change lab `/etc`, packages, `systemctl`, firmware, BMC
- Job files on servers only in `/tmp/lab-job-<name>/`, deleted after
- `helm.sh/helm/v3` is not imported (fights k8s v0.36 / controller-runtime v0.24)

## Lab hop (from another Mac)

Other Macs are not on the VPN. Hop through the VPN Mac, then SSH.

```bash
ssh kartikey0104@<vpn-mac-host>.local 'ssh root@<your-lab-hosts> "hostname && kubectl get dpuoperatorconfig"'
```

On this VPN Mac:

```bash
ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile=$HOME/.ssh/<lab-ssh-key> -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256 \
  root@<your-lab-hosts>
```

## Rebuild PDFs

```bash
/tmp/pdfvenv/bin/python "/Users/kartikey0104/opi_test_report/Lifecycle Manager/generate_pdfs.py"
```
