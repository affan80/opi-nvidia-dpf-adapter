Issue 2 — ServiceFunctionChain status.conditions is rejected on the live apiserver

Date: 2026-10-22
Host: dh2

What the issue was
- DONE WHEN said write DPFReady when the CRD allows it
- The SFC mapping still declared DPFReady. Previous reports treated the
  empty status object as a silent prune
- Live CRD on dh2:

    status: {type: object}   # no properties, no conditions

- Server dry-run of an SFC with status.conditions:

    Error from server (BadRequest): strict decoding error:
    unknown field "status.conditions"

- DataProcessingUnit on the same cluster has status.conditions. Dry-run
  of DPFReady on a DPU: created (server dry run)
- If the controller called Status().Update on SFC, the apiserver would
  reject the request. That is not "when CRD allows it"

What we changed
- status.schema on each mapping: conditions or pruned
- ServiceFunctionChain: schema: pruned
- DataProcessingUnit (NVIDIA and AMD): schema: conditions
- translation_controller.mirrorStatus returns before Status().Update
  when schema is pruned
- Controller test: SFC reconcile does not persist status.conditions
- Validate requires status.schema whenever conditions are declared

How we fixed it
- The DPFReady rule stays in the SFC mapping so flipping schema to
  conditions is the only change when upstream adds the field
- Until then the controller does not write, so there is no BadRequest
  and no pretend-success

Live evidence
    DPU status keys ['conditions']  has conditions True
    SFC status {'type': 'object'}   has conditions False
    SFC + DPFReady → unknown field "status.conditions"
    DPU + DPFReady → created (server dry run)

How to verify
    go test ./pkg/translation/ -count=1
    # on dh2, read-only dry-run of status.conditions on an SFC
    kubectl apply --dry-run=server -f - <<EOF
    # SFC with status.conditions must fail
    EOF
