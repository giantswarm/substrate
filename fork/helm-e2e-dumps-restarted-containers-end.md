---
id: helm-e2e-dumps-restarted-containers-end
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci: dump a restarted container''s end in helm-e2e diagnostics'
upstream:
  outcome: to-file
  refs: []
  exit: 'the dump pull request merged, then the Substrate re-pin drops the commit; fork-only fallback on the same date as kagent-dev/substrate#71'
  class: 5
  destination: 'kagent-dev/substrate helm-e2e.yaml, the Dump diagnostics on failure step (lines 156-163 at 6383ea118)'
  first_step: 'open a pull request on kagent-dev/substrate from the source branch once kagent-dev/substrate#71 merges'
  source: 'upstream/helm-e2e-dump-previous@11bfb4132 (on 6383ea118, +10; reads each restarted container''s previous log by name, where the line''s commit probes --all-containers)'
  test: 'restart an ate-system container mid-suite and fail a lane: the dump prints its termination reason, exit code and previous log, and the Warning events'
  recheck: 2026-11-17
  owner: unassigned
  draft: issuecomment-6301363794
  decision: issuecomment-6301358702
depends_on: [helm-e2e-lanes-run-whatever-an-earlier-lane-did]
verified: 2026-10-20
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
