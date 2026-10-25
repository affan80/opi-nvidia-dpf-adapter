# Issue

`EnsureInstalled` called the detecting installer as soon as the vendor
was known and the Helm settings were pinned. A present `DPU` CRD plus
`v26.4.0` became `VendorOperatorSupported=True` reason `Supported`.

That is a false installation success. DPF v26.4 requires cert-manager
and Argo CD before DPUService integration works. Kamaji is required
when a `DPUCluster` uses `type=kamaji`.

Lab before the fix (2026-10-23):

```
VendorOperatorSupported=True/Supported
  nvidia operator v26.4.0 is within the supported range [v26.4.0, v27.0.0)

ensure installed vendor=nvidia source=spec action=detected
  installed=v26.4.0 skew=Supported ready=true
```

Inventory on the same host:

| Check | Result |
|---|---|
| Kubernetes | `v1.36.4+k3s1` (supported minor) |
| cert-manager CRDs + deploy | present (1/1) |
| Argo CD `applications.argoproj.io` | **ABSENT** |
| Kamaji `tenantcontrolplanes.kamaji.clastix.io` | **ABSENT** |
| `DPUCluster` objects | none (Kamaji not applicable) |
| DPF operator | `dpf-system:v26.4.0` running |

The adapter claimed success because it never asked the cluster about
those prerequisites.
