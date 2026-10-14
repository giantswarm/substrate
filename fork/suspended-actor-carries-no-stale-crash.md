# a suspended actor carries no stale crash record

- **Status:** carried
- **Upstream:** prepared on `upstream/suspended-clears-crash` (the suspend commit); the read migration is the line's own, as upstream's `AGENTS.md` keeps no compatibility with older rows before v1.0.0
- **Our commit:** [#413](https://github.com/giantswarm/substrate/pull/413): a `cherry-pick -x` of the upstream-ready commit and the line's read migration

## Change

`ensureSuspendedFinalized` clears `ActorStatus.crash` when it commits `SUSPENDED`, and its doc comment says so. An Actor row is migrated on read (`cmd/ateapi/internal/store/atepg/legacy_crash.go`, `migrateStaleCrash`, beside the earlier migrations): a crash record on an actor that is not `CRASHED` is dropped. The rewritten row is persisted by the actor's next update; no SQL migration.

## Reason

The suspend commits `SUSPENDED` over an actor an out-of-band transition crashed after its checkpoint completed, with that checkpoint, and kept the crash record, so a healthy `SUSPENDED` actor showed a crash it no longer had. Every reader of the record (the transient-crash recovery, the golden take, the transition reconciler) treats it as the cause of a `CRASHED` actor, and every writer sets it with `CRASHED`; a revert and a transient-crash recovery already clear it when the actor returns to rest. The record is cleared rather than kept as history: a crash a `SUSPENDED` actor carries reads as current to every API client. The read migration clears the records earlier releases left, including a `PAUSED` commit over a crashed actor that kept its crash.
