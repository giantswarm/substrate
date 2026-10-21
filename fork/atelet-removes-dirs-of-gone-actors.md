---
id: atelet-removes-dirs-of-gone-actors
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): remove the directories of actors gone from the control plane'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [atelet-checkpoint-keeps-snapshot-volume-stays-mounted]
verified: 2026-10-21
---
# atelet removes the directories of actors gone from the control plane

## Change

A new `WorkerService.ListGoneActors`, served to an atelet's own client certificate, names the actor UIDs of a request (at most 256) that no stored actor carries (`SELECT uid FROM actors WHERE uid = ANY($1)`); the answer is positive only, and a failed read answers for none. atelet runs a pass at start and every `--actor-dir-gc-period` (default `10m`, `0` disables it): it lists `/var/lib/ate/actors`, skips every entry not named by a UID and every actor whose leftover volumes a sweep still owns (`leftover-volumes.pb`), asks about the rest, and removes the directories of the actors named gone with `removeActorDirs`, as `Terminate` does, logging each; `--actor-dir-gc-dry-run` logs them without removing them. A failed call removes nothing.

## Reason

`Terminate` was the only path that removed an actor's directory, and an actor deleted while it had no node (suspended, its snapshot in the store) is deleted without a terminate anywhere, so the directory it left on the node it last ran on stayed for good with its bundles, checkpoints and state ([#522](https://github.com/giantswarm/substrate/issues/522)). With a Harness that suspends after every turn, every Session deleted by its user leaves one.

## History

not opened yet: the upstream-ready change is on the branch `upstream/atelet-actor-dir-gc` (`0d59c18d`, mirror `main` @ `e2fc0fa9`, DCO-signed; `main` names the volume paths `ExternalVolumeHostPath`, and its `ServiceImpl` takes a pass-through of the new store method). `main` has no sweep of leftover volumes, so the branch skips no actor for one.

[#524](https://github.com/giantswarm/substrate/pull/524): the fork's own commit on the line; [#523](https://github.com/giantswarm/substrate/pull/523): the same change, closed for the merge commit its branch carried
