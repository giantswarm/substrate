# A resume cut by a worker drain answers `Unavailable` naming its crash

- **Status:** carried
- **Upstream:** to file, from the branch `upstream/resume-cut-retryable-code`, stacked on `upstream/resume-restore-cut-by-drain`: upstream's lease honors the caller's context, so there the caller's retry returns the actor to rest
- **Our commit:** [#320](https://github.com/giantswarm/substrate/pull/320): the fork's own commit

## Change

A resume cut by a worker drain answers `Unavailable` naming its crash: `ResumeActor` reads the crash beyond the caller's context, as the crash is recorded, and a crash with a transient cause answers `Unavailable` whether or not the call's own recovery of the actor finished; the actor's return to rest is unchanged (the next attempt or the ActorTransitionReconciler)

## Reason

a resume whose restore the drain cut answered the restore's `Internal`, or the next attempt's `FailedPrecondition` (`AssignWorker prerequisite not met … ACTOR_STATE_CRASHED`), whenever the recovery of the transient crash did not finish within the call — the crash's worker still being torn down, or the call's context ended under the crash — so a caller that retries `Unavailable`, `Aborted` or `ResourceExhausted` gave up although the actor was back at rest seconds later ([#310](https://github.com/giantswarm/substrate/issues/310), [#311](https://github.com/giantswarm/substrate/issues/311))
