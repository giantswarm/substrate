---
id: paused-actor-carries-no-stale-crash
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [suspended-actor-carries-no-stale-crash]
commit: giantswarm/substrate#416
verified: 2026-10-19
---
# a paused actor carries no stale crash record

- **Status:** carried
- **Upstream:** prepared on `upstream/paused-clears-crash`; the line's test of the written row is its own, since upstream has no read migration that hides the record
- **Our commit:** [#416](https://github.com/giantswarm/substrate/pull/416): a `cherry-pick -x` of the upstream-ready commit and the line's write test

## Change

`ensurePausedFinalized` clears `ActorStatus.crash` when it commits `PAUSED`, and keeps an actor whose crash dropped the local checkpoint `CRASHED` with its crash record. Its doc comment states both paths. On this line the second path was already in place; the replay keeps its comment. `TestEnsurePausedFinalized_WritesNoCrashOnPaused` checks the row the commit writes, which a read back cannot show past the read migration of `suspended-actor-carries-no-stale-crash.md`. The persisted format is unchanged; no migration.

## Reason

The pause runs its finalize once the checkpoint completed and commits `PAUSED` over an actor an out-of-band transition crashed meanwhile when the local checkpoint survived, but kept the crash record, so a resumable `PAUSED` actor showed a crash it no longer had. Every reader of the record treats it as the cause of a `CRASHED` actor, and every writer sets it with `CRASHED`; the suspend commit, a revert and a transient-crash recovery clear it when the actor returns to rest. Clearing it is safe only with the checkpoint: upstream, a crash that dropped the checkpoint is committed `PAUSED` with no snapshot, which no resume can serve, so the upstream-ready commit also lets that crash stand.
