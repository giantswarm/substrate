# A resume a rolling worker pool interrupts is retried

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2479](https://github.com/agent-substrate/substrate/pull/2479), from the branch `upstream/session-resume-transient-retry`, stacked on [agent-substrate/substrate#2477](https://github.com/agent-substrate/substrate/pull/2477) (upstream `main` @ `29c049fc`): the recovery and retry of the resume, the shared list with the cancelled worker call; a paused actor keeps its local snapshot and node and has only its worker claim released, since upstream's terminate reaches the snapshot's node. The unreachable-worker cause and the `ActorTransitionReconciler`'s healing stay fork-only: upstream's crash frees the worker without a teardown that could observe it, and upstream has no transition reconciler
- **Our commit:** [#303](https://github.com/giantswarm/substrate/pull/303): the fork's own commit

## Change

A resume a rolling worker pool interrupts is retried (a crash recorded by a resume whose cause a roll produces — the causes of the golden take retry's row above, plus a worker call cancelled under it (a nested gRPC `Canceled`, a `wakeup probe cancelled`) and a crash whose teardown cannot reach the worker any more, which the crash records as `worker unreachable while tearing down the failed actor`; one list in `crash.go` that the golden take retry reads too: `ResumeActor` returns the actor under its lease to the state it was resumed from, PAUSED while it holds its pause snapshot and SUSPENDED otherwise, with its worker torn down and freed and its crash cleared, and resumes it again with backoff, 1 s doubling to 15 s, at most six resumes per call, waiting for a free worker while the pool rolls; a call that runs out answers `Unavailable` with the actor at rest; the `ActorTransitionReconciler` returns to rest such an actor no request recovers; a sandbox killed on a live worker, any other cause, and a crash by any other operation stay terminal)

## Reason

a session actor resumed while the worker pool rolled crashed for good on a draining worker, a missing ateom socket, a cancelled wakeup probe or its worker pod going away mid-restore, although its snapshot was intact; kagent's `ResumeSession` then failed for that conversation until someone reverted the actor by hand, and `RevertActor` drops a pause snapshot ([#297](https://github.com/giantswarm/substrate/issues/297)). Now the session resumes by itself once a worker is up
