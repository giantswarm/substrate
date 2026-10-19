---
id: lifecycle-workflow-outlives-callers-cancellation
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1383, agent-substrate/substrate#1761, agent-substrate/substrate#1914, agent-substrate/substrate#646, agent-substrate/substrate#991]
  exit: upstream PR
depends_on: []
commit: 76a6abc8
verified: 2026-10-19
---
# A lifecycle workflow outlives its caller's cancellation

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/actor-lifecycle-budgets`](https://github.com/giantswarm/substrate/tree/upstream/actor-lifecycle-budgets) here (`40b3d12c` + `861e41d1`, on the mirror `main` @ `007eb1ee`; the messages are the pull-request text). agent-substrate/substrate#646 is the shape (`Canceled` strands actors in RESUMING/SUSPENDING), #1383 the failure; agent-substrate/substrate#991 (closed unmerged 2026-08-25) was the same `WithoutCancel` fix, #1761 and #1914 report the same problems. A team member opens the pull request with DCO sign-off once the team has reviewed it
- **Our commit:** `76a6abc8`, [#22](https://github.com/giantswarm/substrate/pull/22) (rebase-merged 2026-09-12); also [#201](https://github.com/giantswarm/substrate/pull/201) (`fix(ateapi): finish suspends and pauses whose replica died`)

## Change

A lifecycle workflow outlives its caller's cancellation (Resume and Suspend run under `context.WithoutCancel` of the caller's, bounded by `--actor-workflow-deadline`, 5m, and the lease as before); the chart sets the ingress dataplane's request parking (`atenetRouter.requestParking`: budget 30 s, max 1024, on all four `substrateIngress` blocks)

## Reason

a Resume ran under the caller's context, and the caller that gave up was the dataplane: agentgateway's `substrateIngress.requestParking.budget` defaults to 5 s and the chart set nothing, so on every first turn after an idle spell on a node whose cache had evicted the image it cancelled the atelet Restore at 5.0 s (`context canceled`, reason UNKNOWN) after the workflow had durably claimed the worker and moved the actor to `RESUMING`; nothing reclaimed either, and every retry fast-forwarded to the same step and died at the same budget (six retries, each in the same layer). `helm-e2e` pins the old 5 s budget so upstream's parking suite keeps its timing
