# Issue

The LCM could classify nvidia/v26.4.0 but did not pin Helm settings.
Nothing stopped a `DpuOperatorConfig` annotation such as:

- `lifecycle.opi.nvidia.com/chart-url: https://evil.example/chart.tgz`
- `lifecycle.opi.nvidia.com/helm-args: --set image=evil`
- a version pin that is a URL

A later Helm step would have executed those as user-supplied arguments.

Lab before the fix (2026-10-23): adapter healthy, NVIDIA detected,
AMD CRDs absent, LCM does not write Ready, live config has no helm-override
annotations. DPU `Initializing` is DPF runtime, not this bug.
