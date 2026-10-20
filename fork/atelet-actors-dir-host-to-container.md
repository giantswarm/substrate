---
id: atelet-actors-dir-host-to-container
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): mount the actors directory with HostToContainer propagation'
  - 'fix(atelet): leave a volume dir still busy at terminate in place'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [atelet-checkpoint-keeps-snapshot-volume-stays-mounted]
verified: 2026-10-21
---
# atelet mounts the actors directory with HostToContainer propagation

## Change

atelet mounts `/var/lib/ate`, where the CSI node plugin publishes actors' external volumes, with `HostToContainer` propagation, as the worker pods mount the same directory, in the chart and in `manifests/ate-install/atelet.yaml`. `Terminate` no longer fails on a volume directory that cannot be removed: it is logged and left in place with the actor's `volumes` directory, and the rest of the actor's directory is reclaimed; a volume directory is only ever removed empty.

## Reason

Without propagation the bind took a private copy of every mount under `/var/lib/ate` when atelet started, and the copy outlived `NodeUnpublishVolume`: the volume's mount point stayed busy in atelet's mount namespace, `Terminate` failed with `device or resource busy` on every retry, and an actor whose volume was mounted across an atelet restart could never be deleted; its Session stayed `RUNTIME_STATE_DELETING` ([#514](https://github.com/giantswarm/substrate/issues/514)). atelet cannot unmount the copy itself: it runs with `DAC_OVERRIDE` as its only capability. The roll restarts every atelet, which drops the copies of volumes the node already unmounted, so the mounts leaked before the fix go without a record.

## History

not opened yet: the upstream-ready change is on the branch `upstream/atelet-actors-dir-host-to-container` (`70974e7a`, mirror `main` @ `e2fc0fa9`, DCO-signed; `main` names the mount `ate-base` and the volume paths `ExternalVolumeMountsDir` and `ExternalVolumeHostPath`, and the commit splits `resetActorStateDirs` out of `resetActorDirs` there; `main` has no chart test for atelet). It replaces the branch `upstream/atelet-terminate-detaches-busy-volume-dir`, whose lazy detach in atelet cannot work without `CAP_SYS_ADMIN`

[#515](https://github.com/giantswarm/substrate/pull/515): the first attempt, a lazy detach of the busy directory in atelet; closed unmerged

[#521](https://github.com/giantswarm/substrate/pull/521): the fork's own commits on the line
