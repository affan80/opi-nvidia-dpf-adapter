# Issue

`DpuOperatorConfig` delete had no guarded cleanup. A later
implementation could have:

- helm-uninstalled the live lab `dpf-operator` (`owner=helm`)
- deleted shared DPF CRDs while other installations still use them
- deleted a DPU / DPUService still referenced by an OPI object
- deleted `bluefield` in another namespace because the name matched
- retried an unknown cleanup and bypassed the owner check

Hybrid design already says: removing the NVIDIA annotation must not
uninstall DPF; `make undeploy` must not helm-uninstall DPF.
