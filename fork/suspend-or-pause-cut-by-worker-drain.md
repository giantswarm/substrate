---
id: suspend-or-pause-cut-by-worker-drain
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateapi): return a drain-cut checkpoint to the last durable snapshot'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2484]
  exit: as#2484 merged
depends_on: [crash-records-worker-leaving-once-crashs-teardown]
verified: 2026-10-19
---
# A suspend or pause cut by a worker drain returns the actor to its last durable snapshot

## Change

A suspend or pause cut by a worker drain returns the actor to its last durable snapshot: a checkpoint of a running workload whose worker went away (ateom's socket gone, the worker draining, deleted or its pod replaced, the crash's teardown unable to reach it) on an actor holding an external snapshot is recovered like a drain-cut resume, the worker torn down and freed, the half-written snapshot discarded, the pause snapshot dropped, the actor `SUSPENDED` on its external snapshot; `SuspendActor` and `PauseActor` answer `Unavailable` naming the crash, and `ResumeActor` and the ActorTransitionReconciler recover such an actor too. An actor with no external snapshot, a checkpoint failing on a live worker and a paused snapshot's upload stay `CRASHED`

## Reason

a drain that removed the worker under the suspend of a just-resumed actor crashed it for good (`suspend failed: atelet Checkpoint: while calling ateom.CheckpointWorkload: … ateom.sock: connect: no such file or directory (worker unreachable while tearing down the failed actor)`), and the consumer failed the conversation although its last durable snapshot was intact ([#322](https://github.com/giantswarm/substrate/issues/322))

## History

[agent-substrate/substrate#2484](https://github.com/agent-substrate/substrate/pull/2484), from the branch `upstream/checkpoint-cut-by-drain`, stacked on `upstream/resume-cut-retryable-code`: upstream's snapshot records keep the latest completed durable snapshot, which the recovery returns to as `ensureRevertedFinalized` does

[#324](https://github.com/giantswarm/substrate/pull/324): the fork's own commit
