# The ActorTemplate reconciler replaces a golden actor observed `CRASHED` before its snapshot instead …

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/golden-actor-crash-bound`](https://github.com/giantswarm/substrate/tree/upstream/golden-actor-crash-bound) (`0955e7d1` on the mirror `main` @ `c2e2e130`, DCO-signed, no fork references, its message the pull-request text; `golden_actor_crashes` is field 4 there, upstream's next free number of `GoldenSnapshotStatus`, 5 on the line behind the fork-only `workload_boot_failures`, whose bound the patch leaves out; the reconciler tests green on the mirror), queued for review before it goes upstream
- **Our commit:** `release-1.3` only: `9d9895e9` ([#131](https://github.com/giantswarm/substrate/pull/131), first release 1.3.1); the 1.4 line records the crash on the actor and gets its own patch, [#132](https://github.com/giantswarm/substrate/issues/132)

## Change

The ActorTemplate reconciler replaces a golden actor observed `CRASHED` before its snapshot instead of failing the template (counted in the new `GoldenSnapshotStatus.golden_actor_crashes`, persisted first; the crashed golden actor is deleted and a new one booted; past five the template fails `GoldenActorCrashed` with the count; a crash the resume reports with its cause still fails at once)

## Reason

the actor records no crash cause, and a worker deleted under a booting golden actor by a pool roll crashes it like a workload crash: the template failed for good and kept no golden. An upgrade boots the goldens of the templates it renders while it rolls the worker pool; in the lab the first golden after the upgrade lost its draining worker every time ([giantswarm/substrate#105](https://github.com/giantswarm/substrate/issues/105))
