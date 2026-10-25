# Results — 2026-10-23

## Local

`go test ./pkg/lifecycle/ ./pkg/mapping/ ./pkg/translation/` PASS
`make lint` 0 issues

## Lab before change

All green: nvidia Supported, AMD CRD absent, LCM no Ready, hbn chart
from NGC, no helm-override annotations. DPU Initializing is DPF runtime.

## Issue found and fixed

LCM had no pinned Helm settings, so a chart URL / `--set` annotation
would have been available to a later installer.

Fix: `DefaultInstallConfig` + `RejectInstallOverrides` → `InvalidConfig`.

## Lab after image import

| Object | Result |
|---|---|
| `dpu-operator-config` | `True/Supported`, `action=detected` |
| `lcm-evil-helm` (`helm-args: --set image=evil`) | `False/InvalidConfig`, `action=skipped`, installer not run |
| Ready written by LCM | no |
| evil object | deleted after probe |
