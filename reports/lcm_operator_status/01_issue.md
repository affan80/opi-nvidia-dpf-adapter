# Issue

`AssessReadiness.Ready` became true when the `DPU` REST mapping existed.
That ignored controller Deployments and prerequisite Deployments.

The daemon already owns condition type `Ready` on `DpuOperatorConfig`.
The LCM must not write that type.
