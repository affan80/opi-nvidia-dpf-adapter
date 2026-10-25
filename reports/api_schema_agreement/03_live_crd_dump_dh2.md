Live CRD dump — dh2 — 2026-10-23

VPN
- Was Disconnected (last drop 2026-10-22 20:56, F5ChannelError -1073740512)
- Started: scutil --nc start "F5 Access"
- Now Connected, client utun7 = 172.22.100.23
- ping 172.22.1.2: 0% loss, ~330 ms

Cluster
- k3s v1.36.4+k3s1
- dpf-operator-controller-manager = nvcr.io/nvidia/doca/dpf-system:v26.4.0
- Helm release dpf-operator in dpf-operator-system
- helm.sh/resource-policy=keep (CRDs labelled take-ownership, not replaced)

OPI CRDs (created 2026-09-07)

| CRD | Live spec / status | vs pin |
|---|---|---|
| DataProcessingUnit | spec: dpuProductName, isDpuSide, nodeName. status.conditions YES | fixture agrees (nodeName, not dpuNodeName) |
| ServiceFunctionChain | networkFunctions + chart.name + image YES. status EMPTY | chart fields present (PR #6). status.conditions NO — pruned |
| DpuOperatorConfig | spec.logLevel only. spec.vendor NO. status.conditions YES | do not add spec.vendor |

DPF CRDs (created 2026-09-07 unless noted)

| Field | v26.4 fixture | live dh2 |
|---|---|---|
| DPU.spec.bfb | YES (one-of) | YES |
| DPU.spec.serialNumber | YES | YES |
| DPU.spec.dpuNodeName / dpuDeviceName / dpuFlavor / nodeEffect | YES | YES |
| DPU.spec.astraEnabled | YES | **NO** |
| DPUDevice.spec.serialNumber | YES | YES |
| DPUDevice.spec.cluster | YES | **NO** |
| DPUDevice.spec.bmcCredentialSecretName | YES | **NO** |
| BFB.spec.url | YES | YES |
| DPUFlavor.spec.dpuMode | YES | YES |
| DPUService.spec.helmChart.source.{repoURL,chart} | YES | YES |
| DPUService.spec.interfaces / paused | YES | YES |
| DPUService.spec.security.privileged | YES | **NO** |
| DPUServiceInterface.spec.template | YES | YES (CRD created 2026-10-20) |
| DPUServiceChain.spec.template | YES | YES |

Live DPUService spec keys
configPorts, deployInCluster, dpuClusterSelector, helmChart, interfaces,
paused, serviceDaemonSet, serviceID — no security.

Conclusion
Operator image is v26.4.0. Installed provisioning/svc CRDs are still the
2026-09-07 schemas. Mappings stay on the fixtures. This adapter does not
upgrade those CRDs.

No server change. Read-only kubectl get crd.
