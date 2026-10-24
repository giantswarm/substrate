---
id: existing-volume-mounted-per-actor-at-sub
fork: substrate
theme: workspace-volumes
category: feature
status: carried
commits:
  - 'feat: mount an existing volume per actor at a sub-path'
  - 'fix(volume): publish a read-only mount with the volume''s own mode'
  - 'fix(volume): stage a multi-node volume once per node'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1637, agent-substrate/substrate#1988, agent-substrate/substrate#1994]
  exit: as#1988 merged (gated); drop the ungated fork API
depends_on: [read-write-mounts-missing-sub-path-directory, resources-deepequal-unchanged-value-check-generated-declarat, giantswarm/kagent-upstream:session-the-sessions-actor-with-its-workspace-directory]
verified: 2026-10-24
---
# An existing volume mounted per actor at a sub-path

## Change

An existing volume mounted per actor at a sub-path: an ActorTemplate declares it (`Volume.existing_volume`, field 10001, an empty marker), `CreateActor` supplies it (`Actor.existing_volumes`, field 10001, immutable; `ExistingVolume` = name, CSI driver, volume handle, access mode `READ_WRITE_MANY`/`READ_ONLY_MANY` in the shape of upstream's `VolumeAccessMode`, and the `sub_path` the actor sees as the volume's root), and `VolumeMount` gains `sub_path` and `read_only` (fields 10001, 10002), so one volume mounts at several paths. `CreateActor` refuses a reference to a volume the template does not declare as existing, a driver without a `CSIDriverConfig`, a handle no PersistentVolume of the driver holds and an access mode the PersistentVolume does not permit (ate-api-server reads PersistentVolumes: a new ClusterRole rule); the PersistentVolume's `volumeAttributes` reach the driver at resume. An unsupplied existing volume contributes neither itself nor its mounts. Substrate never creates, deletes or detaches one: pause, resume and delete only unmount and mount it; a multi-node volume is staged once per node at one path all its targets share (the CSI spec allows one `staging_target_path` per volume, and csi-driver-host-path refuses a second), each target recorded beside it, and unstaged after the last target that holds it, a staging an earlier release made per target unstaged at its own path; atelet binds each sub-path from a descriptor opened beneath the volume's root without following symbolic links (`openat2` `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS`), so a missing directory or a planted link fails the mount. An actor with existing volumes boots from its image instead of the golden snapshot and cannot be cloned from a tag. Tests: unit (validation, workload spec, CSI staging, the sub-path resolver), the `existingvolumes` e2e on kind with the CSI NFS driver (`E2E_CSI_NFS=1` in pr-workflow)

## Reason

Sessions on one workspace each need their own directory of one read-write-many volume read-write and its git mirrors read-only, hundreds a day; a per-actor volume or snapshot per Session does not scale, and an external volume was created per actor, attached single-node-writer and deleted with it, with no sub-path or read-only mount

## History

follows agent-substrate/substrate#1637 (shared volumes across actors, open), in the shape proposed there (https://github.com/agent-substrate/substrate/issues/1637#issuecomment-6083442678), on top of #1988's access modes (open); upstream puts it behind the Preview gate of #1994 (open), which this line does not carry, so it is ungated here. Exit: drops at the re-pin that carries #1637's API

[#234](https://github.com/giantswarm/substrate/pull/234) (`feat: mount an existing volume per actor at a sub-path`); also [#342](https://github.com/giantswarm/substrate/pull/342) (`fix(volume): publish a read-only mount with the volume's own mode`)

2026-10-24: the per-target staging met a driver that stages: csi-driver-host-path refused an actor's second mount of one workspace volume (its directory and the mirrors) with `volume … is already staged at …`; csi-driver-nfs advertises no staging, so the NFS e2e never staged. Reworked onto one staging per node with the targets recorded; agent-substrate/substrate#1988 stages once per volume already, without the hold of the other targets.

2026-10-20: agent-substrate/substrate#1994 (merged 2026-10-15, `756bb9e1`, not in the v0.5.0-alpha2 pin) adds the Preview gate the gated shape of #1988 needs, not the exit, so the outcome stays open.
