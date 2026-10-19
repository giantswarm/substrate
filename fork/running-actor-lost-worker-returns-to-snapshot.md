---
id: running-actor-lost-worker-returns-to-snapshot
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2484]
  exit: upstream PR after as#2484
depends_on: [suspend-or-pause-cut-by-worker-drain]
commit: giantswarm/substrate#380
verified: 2026-10-19
---
# A running actor whose worker went away returns to its last durable snapshot

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/worker-loss-to-snapshot`, stacked on `upstream/checkpoint-cut-by-drain` (the drain-cut checkpoint's row), where it reads the latest completed durable snapshot of upstream's snapshot records
- **Our commit:** [#380](https://github.com/giantswarm/substrate/pull/380): the fork's own commit; [#396](https://github.com/giantswarm/substrate/pull/396): its e2e tests, also on the upstream branch

## Change

A running actor crashed with no operation in flight because its worker went away under the workload (`worker pod went away while hosting the actor`, `ateom restarted while hosting the actor`, the crashes `crashBoundActor` records) and holding an external snapshot is recovered like a drain-cut checkpoint (`transientWorkerLossCrash`, with it `lostWorkloadCrash`): the worker freed, the pause snapshot and its durable copy dropped, the actor `SUSPENDED` on its external snapshot with the crash cleared. `ResumeActor` then places it on another worker, and the ActorTransitionReconciler recovers one no request does. This return, and the drain-cut checkpoint's, takes only an external snapshot the actor wrote itself (`ownsExternalSnapshot`): an actor that never suspended holds its template's golden snapshot, which carries none of what it did since it booted, so it stays `CRASHED` with its cause rather than reset, as does an actor with no external snapshot.

## Reason

A worker pod deleted under a running actor crashed it for good, and its consumer failed the conversation with "runtime lost", although the snapshot it last resumed from or suspended to was intact ([#334](https://github.com/giantswarm/substrate/issues/334)). The workload went with its worker, so that snapshot is the latest state that survives: the actor comes back with its conversation up to its last checkpoint, never reset; a turn in flight when the worker went away fails and is retried by its consumer.

The e2e suites wait for the state such a crash comes to rest in: `TestLostWorkerReturnsActorToSnapshot` for `SUSPENDED` on the actor's own snapshot, `TestPlatformMetricsEmitted` likewise; `TestRevertCrashedActor` crashes an actor that holds only its golden, which stays `CRASHED` for the revert. `CRASHED` of an actor that owns its snapshot lasts only until the ActorTransitionReconciler's next resync, so a wait for it misses it whenever the crash is recorded just before one ([#387](https://github.com/giantswarm/substrate/issues/387)).
