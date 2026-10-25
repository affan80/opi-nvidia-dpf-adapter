Issue 1 — mapping used Helm chart fields as if they were upstream

Date: 2026-10-22
Host: dh2

What the issue was
- config/mappings/servicefunctionchain.yaml reads item.chart.repository,
  item.chart.name, item.chart.version.
- opiproject/dpu-operator#6 (feat: Helm chart on NetworkFunction) is
  still OPEN. Not merged.
- origin/main NetworkFunction is still:

    type NetworkFunction struct {
        Name  string `json:"name"`
        Image string `json:"image"`  // required, no chart
    }

- config/versions.yaml listed repository github.com/opiproject/dpu-operator
  and commit c107ef71. That commit is on feat/sfc-helm-chart, not main.
  The pin looked like upstream. It was the fork.

What we changed
- config/versions.yaml now says:

    repository: github.com/kartikeyg0104/dpu-operator
    upstream: github.com/opiproject/dpu-operator
    pullRequest: https://github.com/opiproject/dpu-operator/pull/6
    patchState: unmerged
    ref: feat/sfc-helm-chart
    commit: c107ef719f74d4f841ceb1ed45bbd293b3739a3c

- Mapping description: chart is PR #6, not main.
- TestSFCFixturesMatchPinnedHelmChartPatch fails if:
  - pullRequest / patchState=unmerged is missing
  - fixture CRD has no chart.name / repository / version
  - CEL "exactly one of image or chart" is gone
  - mapping when is not 'chart' in item
- Sample: config/samples/servicefunctionchain.yaml
- docs/api-versions.md and README name the unmerged pin.

How we fixed it
- The active API for this adapter is the pinned fork, written down.
- Tests lock the fixtures to that API. Dropping back to main breaks CI.
- Image-only NFs are skipped, not sent to DPF.

Test
- go test ./pkg/mapping/ -run TestSFCFixturesMatchPinnedHelmChartPatch pass
- make verify-api pass
