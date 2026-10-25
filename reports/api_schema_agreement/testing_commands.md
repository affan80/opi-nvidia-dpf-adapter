API / schema agreement — testing commands

Date: 2026-10-23
Host: this Mac (fixtures). Live dh2: VPN down, SSH refused.
Cwd: opi-nvidia-dpf-adapter/

Do not start mapping or pin changes until these pass.

------------------------------------------------
1. Official gate
------------------------------------------------
    make verify-api

What it runs
    go test ./pkg/mapping/ ./pkg/lifecycle/ -count=1 -run \
      'TestMappingFieldsExistInVendoredCRDs|TestSFCFixturesMatchPinnedHelmChartPatch|TestPinnedVersionsAgree|TestPinnedNVIDIAWindowMatchesVersionsYAML'

Result 2026-10-23
    ok  pkg/mapping     0.692s
    ok  pkg/lifecycle   0.361s
    PASS

------------------------------------------------
2. FieldMapping schema vs Go structs
------------------------------------------------
    go test ./pkg/mapping/ -count=1 -v -run \
      'TestSchemaMatchesGoStructs|TestSchemaReservesReadyCondition|TestShippedMappingsDeclareTheSchema'

Result
    PASS  TestSchemaMatchesGoStructs (root, metadata, objectRef, defaults,
          emit, forEach, field, value, statusMapping, statusCondition)
    PASS  TestSchemaReservesReadyCondition
    PASS  TestShippedMappingsDeclareTheSchema
          dataprocessingunit.yaml
          servicefunctionchain.yaml
          amd-dsc200.yaml
          vendor-template/fieldmapping.yaml

------------------------------------------------
3. Condition writer table vs CRD status schema
------------------------------------------------
    go test ./pkg/mapping/ -count=1 -v -run \
      'TestWriterTableSchemaAgreesWithVendoredCRDs|TestShippedMappingsMatchWriterTable|TestValidateRequiresStatusSchema'

Result
    PASS  TestShippedMappingsMatchWriterTable
    PASS  TestWriterTableSchemaAgreesWithVendoredCRDs
    PASS  TestValidateRequiresStatusSchema
    SFC DPFReady stays schema: pruned (live CRD has empty status).

------------------------------------------------
4. Ownership / resource graph
------------------------------------------------
    go test ./pkg/mapping/ -count=1 -v -run \
      'TestSFCMappingIsDirectOwnership|TestNoTwoMappingsOwnTheSameDPFKind'

Result
    PASS  direct SFC: DPUService + DPUServiceInterface + DPUServiceChain
    PASS  no two mappings own the same DPF kind

------------------------------------------------
5. Design contract still records the pins
------------------------------------------------
    go test ./pkg/mapping/ -count=1 -run TestHybridDesignDocIsTheContract

Result
    PASS

------------------------------------------------
6. Runtime NVIDIA window
------------------------------------------------
    go test ./pkg/lifecycle/ -count=1 -v -run \
      'TestPinnedNVIDIAWindowMatchesVersionsYAML|TestSupportedRangeIsWellFormed|TestClassifySkew|TestKnownVendor'

Result
    PASS  window [v26.4.0, v27.0.0) matches config/versions.yaml
    PASS  ClassifySkew: v26.4.0 Supported, below TooOld, v27 TooNew
    PASS  known vendors nvidia / amd

------------------------------------------------
7. Full packages that load the CRDs
------------------------------------------------
    go test ./pkg/mapping/ ./pkg/lifecycle/ ./test/conformance/ -count=1

Result
    ok  pkg/mapping         0.451s
    ok  pkg/lifecycle       0.509s
    ok  test/conformance   10.761s

------------------------------------------------
8. RBAC vs mapping emit kinds
------------------------------------------------
    make verify-rbac

Result
    pkg/translation/zz_generated_rbac.go is up to date

------------------------------------------------
9. Live cluster (read-only) — blocked this run
------------------------------------------------
    # after F5 VPN is Connected
    ping -c 1 172.22.1.2
    ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
        -o IdentityFile="$HOME/.ssh/opi_lab_key" \
        -o UseKeychain=yes -o KexAlgorithms=curve25519-sha256 \
        root@172.22.1.2 'k3s --version; k3s kubectl get crd \
          dataprocessingunits.config.openshift.io \
          servicefunctionchains.config.openshift.io \
          dpuoperatorconfigs.config.openshift.io \
          dpus.provisioning.dpu.nvidia.com \
          dpuservices.svc.dpu.nvidia.com \
          -o custom-columns=NAME:.metadata.name,CREATED:.metadata.creationTimestamp'

Result 2026-10-23 06:15 IST
    ping 172.22.1.2 — 100% loss (VPN Disconnected)

Result 2026-10-23 06:20 IST
    scutil --nc start "F5 Access" → Connected, utun7 172.22.100.23
    ping 172.22.1.2 — 0% loss
    live dump: 03_live_crd_dump_dh2.md

Known live skew from 2026-10-22 (do not "fix" by upgrading lab CRDs)
    DPUService.spec.security.privileged  fixture YES  live NO
    DPU.spec.astraEnabled                fixture YES  live NO
    DPUDevice.spec.cluster               fixture YES  live NO
    SFC status.conditions                fixture NO   live NO  (pruned)

------------------------------------------------
Pins these commands enforce
------------------------------------------------
    Kubernetes     v1.36.4  / go.mod v0.36.0
    OPI            config.openshift.io/v1 @ c107ef71  patchState: unmerged  PR #6
    NVIDIA DPF     v26.4.0  window [v26.4.0, v27.0.0)
    AMD            Unpinned

In-repo copy of this list: docs/api-versions.md (How to re-run the comparison).
