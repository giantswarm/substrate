---
id: atelet-restore-bounded-retried-actor-whose-restore
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1518, agent-substrate/substrate#1562, agent-substrate/substrate#1817, agent-substrate/substrate#1867]
  exit: upstream PR
depends_on: [lifecycle-workflow-outlives-callers-cancellation]
commit: a3b292e4
verified: 2026-10-19
---
# The atelet restore is bounded and retried, and an actor whose restore keeps exceeding its budget is …

- **Status:** carried
- **Upstream:** with the row above on `upstream/actor-lifecycle-budgets` (`861e41d1`); agent-substrate/substrate#1562 (atenet retries `ResumeActor` on gRPC status only) and #1518 (an actor stays `RESUMING` on a runsc error) are its upstream tracks; at the re-pin the reason moves onto alpha1's `ActorStatus.crash` (#1867), the `ateerrors` reasons being deleted (#1817). At the 2026-10-08 re-pin onto v0.4.0-alpha1: upstream classifies atelet errors in `handleAteletError` (`Unavailable`, `Canceled`, `DeadlineExceeded` and a cancelled workflow context are retried, not crashed); `crashOnRestoreFailure` crashes a restore that kept exceeding its budget or hit the workflow deadline and hands every other failure to that classification.
- **Our commit:** `a3b292e4`, [#22](https://github.com/giantswarm/substrate/pull/22); at the v0.0.29 re-pin the patch also passes the budget in upstream's new `workflow_delete_test.go` (#1541)

## Change

The atelet restore is bounded and retried, and an actor whose restore keeps exceeding its budget is failed with the new reason `RESTORE_TIMED_OUT` (`--actor-restore-budget`, default 90 s per attempt with a cold pull and unpack included, 0 = the workflow deadline only; three attempts with a doubling pause within `--actor-workflow-deadline`, the layers an interrupted pull fetched staying cached so a retry makes progress; then the crash directive: CRASHED, worker released, deletable, `ate.actor.crashes` labelled)

## Reason

the Restore step had one attempt and no budget of its own: a restore that ran out of time came back unclassified (reason UNKNOWN), was not retried and left the actor `RESUMING` on a claimed worker that nothing reclaimed, undeletable (FailedPrecondition) until a resume succeeded
