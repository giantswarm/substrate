# A resume never answers RUNNING on a worker that started leaving under its restore

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/resume-worker-leaving-under-restore`
- **Our commit:** the fork's own commit, its pull request named here once open

## Change

`finalizeRunning`, the resume's last step, reads the actor's assigned worker before it commits `RUNNING`. A worker that drained, whose record is gone, or whose pod was replaced while the restore ran (`workerLeavingCause`) is about to take the workload with it, so the resume crashes the actor under its own operation (`resume failed: assigned worker is draining`, `… no longer exists`, `… went away while hosting the actor`) instead. That crash is a transient resume crash: `ResumeActor` returns the actor to the snapshot it was restoring, which it has not moved past, and places it on another worker, or answers `Unavailable` for the caller to retry while no worker is free. Placement itself was already confined to `ACTIVE` workers, in the scheduler and again under the worker's row lock.

## Reason

A session resumed while its worker pool was being replaced was answered `READY` on a worker that the replacement deleted moments later; its next turn found the actor `CRASHED` with `worker pod went away while hosting the actor` ([#383](https://github.com/giantswarm/substrate/issues/383)). The return of such a running actor to its snapshot ([`running-actor-lost-worker-returns-to-snapshot.md`](running-actor-lost-worker-returns-to-snapshot.md)) recovers it afterwards; this change keeps a resume from handing out a worker it already sees leaving.
