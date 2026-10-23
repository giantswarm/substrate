---
id: helm-e2e-dumps-restarted-containers-end
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci: dump a restarted container''s end in helm-e2e diagnostics'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#71]
  exit: 'kagent-dev/substrate#71 merged with the dump commit, then the Substrate re-pin drops the commit; no review by 2026-11-17 makes it fork-only fork infrastructure with #71 kept as the exit'
  class: 5
  destination: 'kagent-dev/substrate#71, as its second commit (same workflow and job as the lanes change)'
  first_step: 'the comment on kagent-dev/substrate#71 asking a maintainer to approve its workflow runs and review it'
  source: 'upstream/helm-e2e-independent-lanes@51a66afaf, a cherry-pick of upstream/helm-e2e-dump-previous@11bfb4132 (+10; reads each restarted container''s previous log by name, where the line''s commit probes --all-containers)'
  test: 'restart an ate-system container mid-suite and fail a lane: the dump prints its termination reason, exit code and previous log, and the Warning events'
  recheck: 2026-11-17
  owner: teemow
  draft: issuecomment-6301363794
  decision: issuecomment-6301358702
depends_on: []
verified: 2026-10-23
---
# helm-e2e's failure dump names a restarted container's end

## Change

On failure, helm-e2e's dump prints every restarted container's termination reason, exit code and finish time, the previous run's log of each `ate-system` pod that has one, and the cluster's warning events.

## Reason

A component that restarts mid-suite (the node's atelet, by a crash or an OOM kill) left only its new run's log in the dump, so the failures it caused carried no cause: the micro-VM lane failed for a day on an atelet restart whose reason no log showed ([#357](https://github.com/giantswarm/substrate/issues/357)). Workflow-only

## History

to file: kagent-dev/substrate's helm-e2e carries the same dump step; agent-substrate/substrate has no helm-e2e. Falls away at the re-pin onto the first release that carries it

[#362](https://github.com/giantswarm/substrate/pull/362)

2026-10-20: placed by the T16 ci-publish-infra theme review (the decision comment is `upstream.decision` on the theme issue): upstream PR on kagent-dev/substrate after #71.

2026-10-23: folded into kagent-dev/substrate#71 as its second commit (cherry-pick of `11bfb4132`) instead of a pull request of its own after #71; same workflow, same job.
