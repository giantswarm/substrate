---
id: atelet-checkpoint-keeps-snapshot-volume-stays-mounted
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): keep a persisted checkpoint whose volume stays mounted'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [existing-volume-mounted-per-actor-at-sub]
verified: 2026-10-20
---
# atelet keeps a persisted checkpoint whose volume stays mounted

## Change

`unmountExternalVolumes` retries a failed unmount with a doubling wait for up to a minute within the caller's context, for `Checkpoint` and `Terminate` alike. Once `Checkpoint` has persisted the snapshot, a volume that still does not unmount is logged, its mount point is left in place and the checkpoint succeeds. `Run` and `Restore` first unmount every external volume whose mount point survived an earlier checkpoint on the node.

## Reason

A failed unmount after the persisted snapshot failed `Checkpoint`. ateapi crashed the actor on it, the snapshot was never committed, and a Session with a workspace lost its conversation at suspend. The target was busy for a while after the sandbox was gone, outside atelet's control, and `Terminate` failed the same way until the mount was let go ([#501](https://github.com/giantswarm/substrate/issues/501)).

## History

not opened yet: the upstream-ready commit is on the branch `upstream/atelet-checkpoint-keeps-snapshot-volume-busy` (`5864c6b5`, mirror `main` @ `e2fc0fa9` + the change, DCO-signed; `main` names the type `Atelet`, the hosts `ExternalVolumeHostPath`)

[#502](https://github.com/giantswarm/substrate/pull/502): the fork's own commit, the same change on the line

[#503](https://github.com/giantswarm/substrate/pull/503): the same commit on `release-1.16`, a `git cherry-pick -x` for `v1.16.4`
