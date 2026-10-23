---
id: helm-e2e-lanes-run-whatever-an-earlier-lane-did
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(helm-e2e): run every lane whatever an earlier lane did'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#71]
  exit: 'kagent-dev/substrate#71 merged, then the Substrate re-pin drops the commit; no review by 2026-11-17 makes it fork-only fork infrastructure with #71 kept as the exit'
  class: 5
  destination: 'kagent-dev/substrate#71, which also carries helm-e2e-dumps-restarted-containers-end'
  first_step: 'comment on kagent-dev/substrate#71 asking a maintainer to approve its workflow runs (action_required) and to review it'
  source: 'upstream/helm-e2e-independent-lanes@51a66afaf (90931ad3c +7, then the dump commit +10; merges cleanly on kagent-dev/substrate main)'
  test: 'make the gVisor lane fail: the later lanes still run and the step that requires every lane reports the failed one'
  recheck: 2026-11-17
  owner: teemow
  draft: issuecomment-6301363233
  decision: issuecomment-6301358702
depends_on: []
verified: 2026-10-23
---
# helm-e2e's lanes each run, whatever an earlier lane did

## Change

Every `Run E2E tests (…)` step of helm-e2e runs once the setup before it succeeded (the `lanes-ready` step), unless the run is cancelled, and reports its own outcome. The job and its name (`e2e-test`) are unchanged.

## Reason

The lanes are steps of one job, so a failed lane skipped every later one: a flake in the gVisor lane hid the micro-VM lane on the same head until a re-run ([#365](https://github.com/giantswarm/substrate/issues/365)). Workflow-only.

## History

[kagent-dev/substrate#71](https://github.com/kagent-dev/substrate/pull/71); agent-substrate/substrate has no helm-e2e. Falls away at the re-pin onto the first release that carries it

[#366](https://github.com/giantswarm/substrate/pull/366)

2026-10-20: placed by the T16 ci-publish-infra theme review (the decision comment is `upstream.decision` on the theme issue): upstream PR kagent-dev/substrate#71, unreviewed since 2026-10-13.

2026-10-23: kagent-dev/substrate#71 also carries the restarted-container dump (helm-e2e-dumps-restarted-containers-end) as its second commit, so both changes share one review.
