---
id: e2e-suite-restarts-atelet-running-actors-node
fork: substrate
theme: test-flakes
category: test
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: none planned
depends_on: []
commit: giantswarm/substrate#179
verified: 2026-10-19
---
# An e2e suite restarts the atelet of a running actor's node and requires the next suspend to commit …

- **Status:** carried
- **Upstream:** test-only, Giant Swarm's own guard for its chart's rollouts; not proposed upstream
- **Our commit:** `giantswarm`: [#179](https://github.com/giantswarm/substrate/pull/179); `release-1.3`: [#173](https://github.com/giantswarm/substrate/pull/173)

## Change

An e2e suite restarts the atelet of a running actor's node and requires the next suspend to commit a snapshot within a client's 30 s budget, twice, the second time on the snapshot the first wrote (`internal/e2e/suites/ateletrestart`, gated by `E2E_ATELET_RESTART=1`, its own `helm-e2e` step)

## Reason

after an atelet DaemonSet roll the first suspend after a turn missed kagent's 30 s quiesce deadline. The atelet restart was not the cause: the 30 s was `runsc delete -force _pause` of a worker without the child-reaper fix above, which the actor's restore happened to follow; atelet and ateapi finished the suspend and committed its snapshot. The suite pins that a roll costs no suspend ([giantswarm/substrate#162](https://github.com/giantswarm/substrate/issues/162))
