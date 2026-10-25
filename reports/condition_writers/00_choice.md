Condition writers — choice

Date: 2026-10-22
Skill: /Superpowers (brainstorming then start working on this)
Host: dh2 (VPN Connected)

Brainstorm
1. Translator also writes Ready
   - Live DpuOperatorConfig printer column is Ready.reason
   - Daemon reseeds Ready every VSP ping
   - Two writers flap the column. Rejected.
2. Translator writes DPFReady/DSCReady always, even on empty status schemas
   - Live SFC: status.conditions is an unknown field (BadRequest, not a
     silent prune). Status().Update would fail or drop the write.
3. Assigned writer table + write only when CRD keeps conditions
   - Ready = vsp-opi-daemon
   - DPFReady on DataProcessingUnit = mapping:dataprocessingunit
   - DSCReady on DataProcessingUnit = mapping:amd-dsc200
   - DPFReady on ServiceFunctionChain = mapping:servicefunctionchain,
     schema=pruned, skip Status().Update
   - VendorOperatorSupported on DpuOperatorConfig = lifecycle

Choice
Option 3. One writer per (object GVK, condition type). Skip the status
update when the source CRD would prune or reject the field.

Unknown input
- Mapping that writes Ready: Spec.Validate fails, controller will not start
- Two mappings / table rows on the same GVK+type: LoadConditionWriterTable
  fails; TestShippedMappingsMatchWriterTable fails
- status.conditions without status.schema: Validate fails
- Ready claimed by anyone except vsp-opi-daemon: table load fails

Done when
config/condition-writers.yaml matches shipped mappings and vendored CRDs.
Live SFC DPFReady is not written. Live DPU DPFReady is allowed.

Reports
- issue_01_ready_collision.md
- issue_02_sfc_status_unknown_field.md
- issue_03_writer_table.md
