# How it was fixed

`DefaultInstallConfig` is the only Helm input:

- vendor nvidia|amd (amd has no chart)
- namespace `dpf-operator-system`
- chart `dpf-operator` @ `v26.4.0` from `https://helm.ngc.nvidia.com/nvidia/doca`
- release `dpf-operator`, owner `opi-nvidia-dpf-adapter`
- `HelmArgs` must be empty

`RejectInstallOverrides` fails closed on chart-url / helm-args / helm-set /
release-namespace / URL version pins. `ApplyVersionPolicy` never copies
those into `InstallConfig`. Condition reason `InvalidConfig`.

`config/versions.yaml` `dpf.helm` must match the Go constants
(`TestInstallConfigMatchesVersionsYAML`).
