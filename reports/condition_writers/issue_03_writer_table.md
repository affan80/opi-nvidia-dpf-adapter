Issue 3 — no assigned-writer table, so collisions were comments

Date: 2026-10-22
Host: dh2

What the issue was
- DONE WHEN: written condition writer table that avoids collision and
  schema pruning
- docs/multi-vendor.md had a two-row table. It did not include SFC
  prune, LCM VendorOperatorSupported, or a test that the table matches
  the mappings and the CRDs
- TestShippedMappingsOwnDistinctConditions only walked mappings. It
  could not fail a mapping that wrote DPFReady on a CRD with no
  conditions[]

What we changed
- config/condition-writers.yaml (source of truth)

    | Object                 | Type                        | Writer                         | Schema      |
    | DataProcessingUnit     | Ready                       | vsp-opi-daemon                 | conditions  |
    | DataProcessingUnit     | DPFReady                    | mapping:dataprocessingunit     | conditions  |
    | DataProcessingUnit     | DSCReady                    | mapping:amd-dsc200             | conditions  |
    | ServiceFunctionChain   | DPFReady                    | mapping:servicefunctionchain   | pruned      |
    | DpuOperatorConfig      | Ready                       | vsp-opi-daemon                 | conditions  |
    | DpuOperatorConfig      | VendorOperatorSupported     | lifecycle                      | conditions  |

- pkg/mapping/condition_writers.go loads the table and rejects
  duplicate (GVK, type) and Ready owned by anyone except the daemon
- Tests: table matches shipped mappings (writer + schema); table schema
  matches vendored CRDs (conditions path present or absent)
- Vendored live DpuOperatorConfig CRD so the table row for Ready /
  VendorOperatorSupported is checked against status.conditions, and so
  envtest can see the printer column is Ready

How we fixed it
- Collision is a failed load, not a comment
- Prune vs write is status.schema, agreed between mapping and table
- Flipping SFC from pruned to conditions is one YAML change on both
  files after upstream adds the field

How to verify
    go test ./pkg/mapping/ -run 'TestLoadConditionWriterTable|TestShippedMappingsMatchWriterTable|TestWriterTableSchemaAgreesWithVendoredCRDs|TestConditionWriterTableRejectsCollision'
    go test ./test/conformance/ -count=1
