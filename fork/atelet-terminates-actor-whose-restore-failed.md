---
id: atelet-terminates-actor-whose-restore-failed
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
commit: giantswarm/substrate#436
verified: 2026-10-19
---
# atelet terminates an actor whose restore failed

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/terminate-after-failed-restore` (`8e130f56`, mirror `main` @ `5cf1c245` + the change, DCO-signed; `main` names the target `WorkerPodUid` and the snapshot `Fidelity`)
- **Our commit:** [#436](https://github.com/giantswarm/substrate/pull/436): the fork's own commit, a `git cherry-pick -x` of the same change on the pin

## Change

`Restore` records the actor's sandbox (`sandbox-assets.json`) before it calls ateom `RestoreWorkload`, as `Run` does, so a restore that fails in ateom leaves the record a later `Terminate` tears the actor down with. `Terminate` treats a missing record as nothing to tear down on ateom: it logs that, skips `TerminateWorkload`, and goes on with the volume, checkpoint and directory cleanup. ateom cleans up a `RestoreWorkload` it fails itself.

## Reason

`Restore` recorded the sandbox only after a successful restore, and `Terminate` failed without a record. An actor whose sandbox died during `runsc restore` could then be neither reverted (`RevertActor` stayed in REVERTING) nor deleted (`FailedPrecondition: … could not be torn down`), and kagent's session was lost for good ([#435](https://github.com/giantswarm/substrate/issues/435), [giantswarm/kagent-upstream#578](https://github.com/giantswarm/kagent-upstream/issues/578)).
