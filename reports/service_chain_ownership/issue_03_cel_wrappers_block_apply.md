Issue 3 — CEL list/map wrappers could not be applied

Date: 2026-10-22

What the issue was
- The chain needs a list of switches built from networkFunctions[].
  The interface needs a labels map keyed by the NF name.
- Both are CEL expressions. cel-go returned ref.Val wrappers:
  map[ref.Val]ref.Val and []ref.Val
- Server-side apply then failed:

    apply DPUServiceInterface/hbn-ifc: failed to marshal apply
    configuration: json: unsupported type: map[ref.Val]ref.Val

- ConvertToNative(interface{}) was not enough. Nested maps and lists
  stayed as CEL types.

What we changed
- pkg/mapping/cel.go: celNative recursively unwraps ref.Val, []ref.Val,
  and map[ref.Val]ref.Val into []any / map[string]any / string / bool /
  int64 / float64
- evalCEL returns that native value
- TestSFCApplyEmitsOwnedGraph checks the interface label is the Go
  string "hbn" and the chain has one switch

How we fixed it
- The mapping still uses CEL for the list/map (no Go switch on Kind).
- Only the interpreter unwraps types so unstructured JSON can marshal.

How to verify
    go test ./pkg/mapping/ -run TestSFCApplyEmitsOwnedGraph
    go test ./pkg/translation/ -count=1
