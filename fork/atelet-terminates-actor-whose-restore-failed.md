# atelet terminates an actor whose restore failed

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/terminate-after-failed-restore`, on the mirror `main` at the pin (v0.5.0-alpha2)
- **Our commit:** [#436](https://github.com/giantswarm/substrate/pull/436): the fork's own commit, a `git cherry-pick -x` of the upstream-ready one (`bcb17f9b`)

## Change

`Restore` records the actor's sandbox (`sandbox-assets.json`) before it calls ateom `RestoreWorkload`, as `Run` does, so a restore that fails in ateom leaves the record a later `Terminate` tears the actor down with. `Terminate` treats a missing record as nothing to tear down on ateom: it logs that, skips `TerminateWorkload`, and goes on with the volume, checkpoint and directory cleanup. ateom cleans up a `RestoreWorkload` it fails itself.

## Reason

`Restore` recorded the sandbox only after a successful restore, and `Terminate` failed without a record. An actor whose sandbox died during `runsc restore` could then be neither reverted (`RevertActor` stayed in REVERTING) nor deleted (`FailedPrecondition: … could not be torn down`), and kagent's session was lost for good ([#435](https://github.com/giantswarm/substrate/issues/435), [giantswarm/kagent-upstream#578](https://github.com/giantswarm/kagent-upstream/issues/578)).
