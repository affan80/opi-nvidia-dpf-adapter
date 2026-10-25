# Issue

## What was missing

`EnsureInstalled` could detect CRDs and observe Helm storage. It had
no Operator Lifecycle Manager path. Version therefore had to come
from the DPF image window or
`lifecycle.opi.nvidia.com/vendor-operator-version`.

A later step could have:

1. Installed OLM on this k3s lab (a cluster change this repo must not make).
2. Invented an OperatorHub package for DPF (NVIDIA does not publish one).
3. Created a Subscription next to the live Helm `dpf-operator`.

## What the latest docs say

NVIDIA DPF Host Trusted v26.4.0 and v26.4.1:

```
helm upgrade --install -n dpf-operator-system dpf-operator \
  dpf-repository/dpf-operator --version=v26.4.0
```

https://networking-docs.nvidia.com/dpf/26.4.0/dpf-host-trusted
https://networking-docs.nvidia.com/dpf/26.4.1/dpf-host-trusted

OKD's DPF Operator page also uses that Helm registry, not a
Subscription.

## Live lab (before the fix)

```
subscriptions.operators.coreos.com          NotFound
clusterserviceversions.operators.coreos.com NotFound
sh.helm.release.v1.dpf-operator.v1          owner=helm
VendorOperatorSupported=True/Supported
VendorOperatorReady=True/Ready
```

OLM is not missing because the code forgot a catalog. The package
manager on this cluster is Helm.

## Done-when

- Missing `operators.coreos.com` is not an error.
- A present Subscription/CSV at `dpf-operator-system/dpf-operator`
  supplies the version (human pin still wins).
- Helm storage, when present, still wins.
- No Subscription is created.
- AMD stays skipped (no package).
