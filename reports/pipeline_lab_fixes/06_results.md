# Combined pipeline retest

## First pass — 2026-10-23T14:11:16Z

Adapter translation and DPF system Ready. DPU blocked on
`DPUClusterNotAllocated`. No Argo Applications.

## Second pass — after static DPUCluster (2026-10-23T14:26Z–14:28Z)

| Object | Result |
|---|---|
| `DPUCluster/dpu-cluster-1` | **Ready**, `HealthCheckPassed`, `v1.36.4+k3s1`, `nodesCount=1` |
| `DPU/dh2-bluefield-2` | **OS Installing** — Initialized/BFB/FW/Interface True; `OSInstalled=False/InstallationInProgress` |
| Argo Applications | **11 created by DPF** including `dpu-cluster-1-hbn` |
| `argocd-initial-admin-secret` | GONE |
| Argo server | ClusterIP |
| LCM | `VendorOperatorSupported=True/Supported` |
| `hbn` chart | `v26.4.0` |
| PCI annotation | `0000-0f-00` |
| `DPFOperatorConfig` Ready | **False/Pending during OS install** — DPU-targeted system DPUServices (flannel, sriov, sfc-controller, cni-installer, …) wait for the DPU node |
| SFC `DPFReady` | **False** while `hbn` Application Sync=Unknown (DPU not joined yet). Reason string stays `AllServicesReady` (static mapping text) |

OS install can run 30–45 minutes (`osInstallTimeout: 45m` on the
config). That is the live flash, not a missing object.

Argo Applications were **not** created by hand.
