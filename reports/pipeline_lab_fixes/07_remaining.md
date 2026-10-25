# Remaining — in progress or accepted

## 1. DPU `OS Installing`

Not a missing CR. Hostagent is flashing `bf-bundle-340` onto BlueField-2.
Wait:

```bash
k3s kubectl -n dpf-operator-system get dpu dh2-bluefield-2 -w
```

When `OSInstalled=True` the DPU should reboot the Arm cores and try to
join this k3s. Then Argo apps (flannel, hbn, …) can go Healthy and
`DPFOperatorConfig` can return to Ready.

BlueField-2 is off-spec vs DPF v26.4 host-trusted (docs want BF3).
Install can still fail on capacity (`InterfaceInitialized` warned
unable to check DPU CPU/Memory).

## 2. Dummy `br-dpu`

Runtime stub, metric 10000. Reboot drops it. Recreate with the same
`ip link` / `ip route` commands if the node restarts.

## 3. Control-plane label off

Still required for detector on this single node.

## 4. CRD CEL vs operator image

Unchanged. Lab CRD does not require privileged on create. Production
should match operator and CRD instead.

## 5. ApplicationSet replicas 0

DPF chart default. Individual Applications exist without it.

## 6. Kamaji

Still not installed. Static DPUCluster uses host k3s instead. See
vulnerability 9 in `03_vulnerabilities.md`.
