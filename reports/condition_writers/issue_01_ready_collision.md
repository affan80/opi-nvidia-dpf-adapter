Issue 1 — two writers on Ready would flap hardware status

Date: 2026-10-22
Host: dh2

What the issue was
- VSP/OPI daemon owns Ready on DataProcessingUnit and DpuOperatorConfig
  (plugin-init + VSP ping).
- Live DpuOperatorConfig printer column:

    jsonPath: .status.conditions[?(@.type=='Ready')].reason
    name: Status

- Translation upserts conditions by type. A mapping that also wrote Ready
  would overwrite the daemon every reconcile. kubectl get would show the
  translator's value, not hardware Ready.
- NVIDIA and AMD both watch DataProcessingUnit. If both wrote the same
  type they would overwrite each other even if they avoided Ready.

What we changed
- config/condition-writers.yaml: Ready writer is always vsp-opi-daemon
- Mappings write DPFReady (NVIDIA) and DSCReady (AMD), never Ready
- Spec.Validate still rejects Ready (any capitalization)
- mergeConditions skips Ready even if a mapping slipped past Validate
- TestConditionWriterTableRejectsNonDaemonReady
- TestApplyStatusDoesNotWriteDaemonReady
- Conformance already asserts Ready is absent on sources the translator
  reconciles, and LCM does not write Ready on DpuOperatorConfig

How we fixed it
- One type, one writer. Daemon keeps Ready. Translator keeps vendor-scoped
  types. Different vendors cannot share DPFReady.

How to verify
    go test ./pkg/mapping/ -run 'TestConditionWriterTable|TestShippedMappingsOwnDistinctConditions|TestValidateRejectsReservedCondition'
    go test ./pkg/translation/ -run TestApplyStatusDoesNotWriteDaemonReady
    go test ./pkg/lifecycle/ -run TestSupportedConditionIsNotReady
