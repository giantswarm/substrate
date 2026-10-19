---
id: actortemplate-reconciler-retries-golden-take-rolling-worker
fork: substrate
theme: golden-boot
category: bugfix
status: carried
commits:
  - 'fix(ateapi): retry a golden take a rolling worker pool interrupts'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2477]
  exit: as#2477 merged; the clearing of old state stays fork-only
depends_on: []
verified: 2026-10-19
---
# The ActorTemplate reconciler retries a golden take a rolling worker pool interrupts

## Change

The ActorTemplate reconciler retries a golden take a rolling worker pool interrupts (a golden actor crash whose recorded cause a roll produces — the worker drained, went away or no longer hosts it, the worker pod or ateom went away, atelet without an IP or with two pods on the node, the ateom socket not listening, a failed dial — deletes the golden actor, which frees its worker, and requeues the template with the work queue's backoff, capped at a minute, recording nothing; a `GoldenActorCrashed` error with such a cause, recorded by a release before the retry, is cleared and the golden taken again, the recorded golden staying the template's until the new one is complete; any other crash cause still fails the template)

## Reason

the 1.7.0 to 1.8.1 step started the re-take of every golden while the worker pool and the atelet DaemonSet still rolled: every golden actor crashed on a draining worker or a missing ateom socket within seconds, every template recorded a terminal `GoldenActorCrashed` error, nothing retried it, and the Control service had no way to clear it short of deleting the template ([#292](https://github.com/giantswarm/substrate/issues/292); the 1.4 line's form of [#132](https://github.com/giantswarm/substrate/issues/132)). Now the take converges within a minute of the pool settling, and rolling a release with the retry heals the templates an earlier step failed

## History

[agent-substrate/substrate#2477](https://github.com/agent-substrate/substrate/pull/2477), from the branch `upstream/golden-transient-retry` on upstream `main` @ `29c049fc`: the retry of the transient crash and the one-minute backoff cap; the clearing of a failure an earlier release recorded stays fork-only, since upstream records no crash cause on the template and asks for no fallbacks for old state before v1.0.0

[#294](https://github.com/giantswarm/substrate/pull/294): the fork's own commit
