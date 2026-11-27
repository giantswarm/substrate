---
id: e2e-suite-restarts-atelet-running-actors-node
fork: substrate
theme: test-flakes
category: test
status: dropped
commits:
  - 'test(e2e): require the first suspend after an atelet restart to commit'
upstream:
  outcome: to-file
  refs: []
  exit: "re-port onto the v1.0.1 line: needs a rewrite against async suspend and `Snapshot.durable_snapshot.state`, plus its CI job"
depends_on: []
verified: 2026-11-27
---
# An e2e suite restarts the atelet of a running actor's node and requires the next suspend to commit …

## Change

An e2e suite restarts the atelet of a running actor's node and requires the next suspend to commit a snapshot within a client's 30 s budget, twice, the second time on the snapshot the first wrote (`internal/e2e/suites/ateletrestart`, gated by `E2E_ATELET_RESTART=1`, its own `helm-e2e` step)

## Reason

after an atelet DaemonSet roll the first suspend after a turn missed kagent's 30 s quiesce deadline. The atelet restart was not the cause: the 30 s was `runsc delete -force _pause` of a worker without the child-reaper fix above, which the actor's restore happened to follow; atelet and ateapi finished the suspend and committed its snapshot. The suite pins that a roll costs no suspend ([giantswarm/substrate#162](https://github.com/giantswarm/substrate/issues/162))

## History

test-only, Giant Swarm's own guard for its chart's rollouts; not proposed upstream

`giantswarm`: [#179](https://github.com/giantswarm/substrate/pull/179); `release-1.3`: [#173](https://github.com/giantswarm/substrate/pull/173)

2026-11-27: not replayed at the re-pin onto kagent-dev/substrate v1.0.1 (`release-1.0@7ce6f439`): needs a rewrite against async suspend and `Snapshot.durable_snapshot.state`, plus its CI job. Recorded as open in the re-pin record (FORK.md).
