---
id: atelet-checkpoint-keeps-snapshot-volume-stays-mounted
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): keep a persisted checkpoint whose volume stays mounted'
  - 'fix(atelet): keep the snapshot only when a shared volume stays mounted'
  - 'fix(atelet): sweep the volumes a checkpoint left mounted'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [existing-volume-mounted-per-actor-at-sub]
verified: 2026-10-20
---
# atelet keeps a persisted checkpoint whose volume stays mounted

## Change

`unmountExternalVolumes` retries a failed unmount with a doubling wait for up to a minute within the caller's context, for `Checkpoint` and `Terminate` alike. Once `Checkpoint` has persisted the snapshot, a shared volume (`READ_WRITE_MANY` or `READ_ONLY_MANY`, which other nodes may mount meanwhile) that still does not unmount is logged, its mount point is left in place and the checkpoint succeeds, and a background sweep keeps unmounting it (first after a minute, then doubling up to 15 minutes) and removes its mount point once it lets go, since a suspended actor is deleted without a terminate on the node; its record `leftover-volumes.pb` in the actor's directory resumes the sweep after an atelet restart; a `READ_WRITE_ONCE` volume that stays mounted still fails it, since the suspend detaches that volume from the node next. `Run`, `Restore` and `Terminate` first stop the actor's sweep, and `Run` and `Restore` unmount every external volume whose mount point survived an earlier checkpoint on the node.

## Reason

A failed unmount after the persisted snapshot failed `Checkpoint`. ateapi crashed the actor on it, the snapshot was never committed, and a Session with a workspace lost its conversation at suspend. The target was busy for a while after the sandbox was gone, outside atelet's control, and `Terminate` failed the same way until the mount was let go ([#501](https://github.com/giantswarm/substrate/issues/501)).

## History

not opened yet: the upstream-ready change is on the branch `upstream/atelet-checkpoint-keeps-snapshot-volume-busy` (`5864c6b5` and `93b55ff0`, squashed into one commit when it is opened; mirror `main` @ `e2fc0fa9`, DCO-signed; `main` names the type `Atelet`, the hosts `ExternalVolumeHostPath`). `main` has no shared access modes yet (agent-substrate/substrate#1988, open), so there every external volume is single-node-writer and a checkpoint still fails while one stays mounted; the upstream change is the retried unmount and the cleanup of a leftover mount point, and keeping the snapshot for a shared volume follows agent-substrate/substrate#1988

[#502](https://github.com/giantswarm/substrate/pull/502): the fork's own commit, the same change on the line

[#503](https://github.com/giantswarm/substrate/pull/503): the same commit on `release-1.16`, a `git cherry-pick -x` for `v1.16.4`

[#509](https://github.com/giantswarm/substrate/pull/509): the sweep of the volumes a checkpoint left mounted on `release-1.16`, a `git cherry-pick -x` of the line's commit for `v1.16.5`
