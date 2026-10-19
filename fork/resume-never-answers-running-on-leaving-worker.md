---
id: resume-never-answers-running-on-leaving-worker
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2480]
  exit: upstream PR after as#2480
depends_on: [crash-records-worker-leaving-once-crashs-teardown, running-actor-lost-worker-returns-to-snapshot]
commit: giantswarm/substrate#384
verified: 2026-10-19
---
# A resume never answers RUNNING on a worker that started leaving under its restore

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/resume-worker-leaving-under-restore`, stacked on `upstream/resume-restore-cut-by-drain` ([agent-substrate/substrate#2480](https://github.com/agent-substrate/substrate/pull/2480)), whose `workerLeavingCause` it reads
- **Our commit:** [#384](https://github.com/giantswarm/substrate/pull/384): the fork's own commit

## Change

`finalizeRunning`, the resume's last step, reads the actor's assigned worker before it commits `RUNNING`. A worker that drained, whose record is gone, or whose pod was replaced while the restore ran (`workerLeavingCause`) is about to take the workload with it, so the resume crashes the actor under its own operation (`resume failed: assigned worker is draining`, `… no longer exists`, `… went away while hosting the actor`) instead. That crash is a transient resume crash: `ResumeActor` returns the actor to the snapshot it was restoring, which it has not moved past, and places it on another worker, or answers `Unavailable` for the caller to retry while no worker is free. Placement itself was already confined to `ACTIVE` workers, in the scheduler and again under the worker's row lock.

## Reason

A session resumed while its worker pool was being replaced was answered `READY` on a worker that the replacement deleted moments later; its next turn found the actor `CRASHED` with `worker pod went away while hosting the actor` ([#383](https://github.com/giantswarm/substrate/issues/383)). The return of such a running actor to its snapshot ([`running-actor-lost-worker-returns-to-snapshot.md`](running-actor-lost-worker-returns-to-snapshot.md)) recovers it afterwards; this change keeps a resume from handing out a worker it already sees leaving.
