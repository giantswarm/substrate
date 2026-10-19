---
id: read-write-mounts-missing-sub-path-directory
fork: substrate
theme: workspace-volumes
category: feature
status: carried
upstream:
  outcome: fork-only
  refs: [agent-substrate/substrate#1637]
  exit: as#1637 resolution
depends_on: [existing-volume-mounted-per-actor-at-sub]
commit: giantswarm/substrate#239
verified: 2026-10-19
---
# A read-write mount's missing `sub_path` directory is created at the bind

- **Status:** carried
- **Upstream:** amends the row above's patch and follows agent-substrate/substrate#1637 (open) in its shape. Exit: drops with the row above at the re-pin that carries #1637's API
- **Our commit:** [#239](https://github.com/giantswarm/substrate/pull/239) (`fix(volumebind): create a missing read-write sub_path at the bind`)

## Change

A read-write mount's missing `sub_path` directory is created at the bind: ateom's `volumebind.Prepare` makes the last component with `mkdirat` beneath the parent opened like the bind resolves a sub-path (`RESOLVE_BENEATH\|RESOLVE_NO_SYMLINKS\|RESOLVE_NO_XDEV`), owned by the user the mounting container runs as (read from its OCI bundle) and `0770` whatever the umask, every creation before any bind; a missing parent, a symbolic link in the path, a file under the name, a read-only mount, a `READ_ONLY_MANY` volume and a read-only file system keep failing with the reason. `MountDir` moves to `ocispec`. Existing-volume declarations and their mounts are exempt from the unchanged-volumes check of a template repoint, like `system_info` volumes. Tests: `TestCreateVolumeDir`, `TestProcessUser`, `TestValidateTemplateVolumesUnchanged`, `TestUpdateActor_RepointTemplate`; the `existingvolumes` e2e leaves one session directory for Substrate to create and checks its mode

## Reason

a caller that assigns the directory name inside its own create call (kagent's `CreateSession` picks the Session id and mounts `sessions/<id>/` of the workspace volume) cannot make the directory before the actor exists, so every Session with a volume source failed at its first resume; a root-owned directory stays unwritable for a harness that runs as its image's `USER`; adding an existing-volume slot to a template made every actor's repoint `FailedPrecondition` ([#238](https://github.com/giantswarm/substrate/issues/238))
