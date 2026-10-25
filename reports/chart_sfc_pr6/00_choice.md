Chart-based SFC — choice

Date: 2026-10-22
Skill: /Superpowers (brainstorming then start working on this)

Brainstorm
1. Wait for opiproject/dpu-operator#6 to merge
   - Mapping would be invalid on every cluster until then
2. Silently keep using chart in fixtures and hope they match main
   - That is the defect. Upstream main still has required image and no chart
3. Pin the unmerged PR as a named fork/patch, vendor that CRD, fail tests
   if fixtures regress to image-only main

Choice
Pin github.com/kartikeyg0104/dpu-operator feat/sfc-helm-chart
commit c107ef71, pullRequest opiproject/dpu-operator#6, patchState unmerged.
Mapping emits only when 'chart' in item. Unknown mix (image and chart
together) is rejected by the CRD CEL rule.

Done when
Chart ServiceFunctionChain admitted on dh2. Fixtures match that patch.
Tests fail if the pin is dropped.
