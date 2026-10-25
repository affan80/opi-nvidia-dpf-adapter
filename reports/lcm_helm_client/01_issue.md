# Issue

## What was missing

`EnsureInstalled` already had:

- a pinned `InstallConfig` (`pkg/lifecycle/config.go`)
- a detecting installer (`pkg/lifecycle/installer.go`) that only REST-maps `DPU` / `DSCDevice`
- DPF Helm prerequisites (cert-manager, Argo CD; Kamaji only if type=kamaji)

It did **not** have a Helm client. Without that, a later step could:

1. Shell out to `helm` with user `--set`, a chart URL, or a second release name.
2. Install `dpf-operator` again next to the existing lab release.
3. Adopt the live lab release even though Helm, not this adapter, owns it.

## Why adopt is dangerous on this lab

Live secret:

```
sh.helm.release.v1.dpf-operator.v1
  labels: name=dpf-operator, owner=helm, status=deployed, version=1
```

Owner is `helm`, not `opi-nvidia-dpf-adapter`. Adopting would
`helm upgrade` production DPF (image `nvcr.io/nvidia/doca/dpf-system:v26.4.0`)
and could delete or rewrite the running operator.

`DetectingInstaller` must stay mutation-free. Helm belongs in a **new**
file, not in `installer.go`. Tests already ban `helm.New`,
`exec.Command("helm"`, and `HelmClient` **inside** `installer.go`.

## Done-when

Fresh-install and already-installed test cases converge on **one**
release identity:

```
dpf-operator-system/dpf-operator@v26.4.0
```

plus owner label `lifecycle.opi.nvidia.com/owner=opi-nvidia-dpf-adapter`.
