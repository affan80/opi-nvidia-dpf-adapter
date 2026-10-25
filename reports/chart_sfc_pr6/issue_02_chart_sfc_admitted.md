Issue 2 — prove a chart ServiceFunctionChain is admitted

Date: 2026-10-22
Host: dh2

What the issue was
- DONE WHEN: Chart-containing ServiceFunctionChain is admitted and
  fixtures match the active API.
- No SFC objects existed on dh2.
- Needed a live apply, not only a unit test.

What we changed
- Applied a chart-only SFC on dh2 (same shape as
  config/samples/servicefunctionchain.yaml):

    spec.networkFunctions[0].chart.{name,repository,version}

What we saw
- Created: servicefunctionchain/sfc-chart-probe
- Stored spec kept chart.name=hbn, repository, version. Not pruned.
- Image+chart together was rejected:

    spec.networkFunctions[0]: Invalid value:
    exactly one of image or chart must be set

- That is the PR #6 CEL rule on the live CRD. Lab CRD matches the pin.

How we fixed it
- Admission is the CRD. The pin and the sample are the repo side.
- Translation to DPUService is a separate issue (see issue_03).

Test
- kubectl apply of chart SFC: created
- kubectl apply of image+chart: invalid
- TestSFCFixturesMatchPinnedHelmChartPatch pass
