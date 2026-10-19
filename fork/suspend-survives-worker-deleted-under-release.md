---
id: suspend-survives-worker-deleted-under-release
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateapi): a release whose worker was deleted under it succeeds'
upstream:
  outcome: none
  refs: []
  exit: prepare and file
depends_on: []
verified: 2026-10-19
---
# A suspend survives its worker's record being deleted under the release

## Change

`releaseWorker` treats a worker that is gone by the time `ReleaseActorFromWorker` locks it the same way as a worker its read already found gone: nothing is left to release (the worker's delete cascades its assignments), and the release succeeds. Before, the store's `ErrNotFound` reached the caller wrapped, and `SuspendActor` (likewise `PauseActor`, `ResumeActor`) maps any `ErrNotFound` to `NotFound "Actor … not found"`. `FinalizeSuspended` now commits the checkpoint it took and answers with the `SUSPENDED` actor. Crash, delete and revert share the release and gain the same.

## Reason

On a micro-VM worker the checkpoint stops the sandbox, so a pod deleted during a suspend goes away as soon as the checkpoint returns. The worker's delete (ate-controller's `DeleteWorker`) then lands between `FinalizeSuspended`'s read of the worker and its release, and a suspend of a `RUNNING` actor answered that the actor does not exist (`TestGracefulWorkerTerminationSuspend`, helm-e2e micro-VM lane, [#402](https://github.com/giantswarm/substrate/issues/402)). `TestEnsureSuspendedFinalized_WorkerDeletedUnderRelease` runs the real `DeleteWorker` workflow in that window.

## History

none opened

[#405](https://github.com/giantswarm/substrate/pull/405): the fork's own commit
