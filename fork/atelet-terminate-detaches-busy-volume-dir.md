---
id: atelet-terminate-detaches-busy-volume-dir
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): detach a volume dir still busy after its unmount at terminate'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [atelet-checkpoint-keeps-snapshot-volume-stays-mounted]
verified: 2026-10-20
---
# atelet's terminate detaches a volume dir still busy after its unmount

## Change

Once `Terminate` has unmounted the actor's external volumes, `removeActorDirs` removes each volume directory; one that does not remove still holds a mount, a stale one or one stacked under the mount the unmount took, and every mount at or below it is detached lazily (`MNT_DETACH`), then the removal is tried once more. A volume directory is only ever removed empty, so no mounted data is deleted. One that still cannot be removed is logged and left in place with the actor's `volumes` directory, the rest of the actor's directory is reclaimed, and the terminate succeeds. This is also the sweep of the volume mounts leaked before the checkpoint fix, which left no `leftover-volumes.pb` record: every entry of the actor's `volumes` directory is swept, in the spec or not.

## Reason

An actor that crashed while an external volume stayed mounted, under a release before the checkpoint fix, was remounted on top of the stale mount by its next activation. At delete, `NodeUnpublishVolume` answered OK for its volumes, and the removal of the volume directory failed with `device or resource busy`: `Terminate` failed on every retry, ate-controller kept the actor, and its Session stayed `RUNTIME_STATE_DELETING` for good ([#514](https://github.com/giantswarm/substrate/issues/514)).

## History

not opened yet: the upstream-ready change is on the branch `upstream/atelet-terminate-detaches-busy-volume-dir`

[#515](https://github.com/giantswarm/substrate/pull/515): the fork's own commit on the line
