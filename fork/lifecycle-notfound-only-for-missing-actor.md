# NotFound on Pause, Resume, Suspend and Revert only for a missing actor

- **Status:** carried
- **Upstream:** none opened
- **Our commit:** the fork's own commit

## Change

`PauseActor`, `ResumeActor`, `SuspendActor` and `RevertActor` map a workflow's `store.ErrNotFound` through one helper, `lifecycleError`: it re-reads the actor and answers `NotFound "Actor … not found"` only when the actor's own record is gone. Any other missing record (its worker, a worker's claim) answers the named `errLifecycleRecordGone`, `Aborted`, with the store error in its message; the answer no longer matches `store.ErrNotFound`. `ensurePausedFinalized` releases its worker through `releaseWorker`, as suspend, crash, delete and revert do, so a worker deleted under the release is no error; when that delete crashed the actor and dropped its local checkpoint, the crash stands instead of a `PAUSED` commit with no snapshot. `ensureSuspendedFinalized`'s doc comment states what the code does: it commits `SUSPENDED` over an actor crashed after its checkpoint completed.

## Reason

Before, the handlers mapped any `store.ErrNotFound` to `NotFound`, so a worker or claim lookup that lost a race told the caller its actor does not exist; the suspend's release path was fixed alone, and the pause's release still returned the store's `ErrNotFound`. `TestLifecycleRPCs_NotFoundOnlyForMissingActor` covers each RPC, `TestEnsurePausedFinalized_WorkerDeletedUnderRelease` runs the real `DeleteWorker` workflow in the pause's release window.
