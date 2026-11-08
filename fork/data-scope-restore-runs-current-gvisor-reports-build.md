---
id: data-scope-restore-runs-current-gvisor-reports-build
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): run data-scope restores on the current gVisor, report it'
upstream:
  outcome: fork-only
  refs: []
  exit: leaves with pin-gvisor-nightly-2026-09-09 once upstream restores a checkpoint image on the build that wrote it and reports the build an actor runs on
depends_on: [pin-gvisor-nightly-2026-09-09]
verified: 2026-11-08
---
# A data-scope restore runs the current gVisor, and an actor reports its build

## Change

The atelet's `restoreRuntime` (`cmd/atelet/sandbox_assets.go`) runs the request's sandbox binaries, the
SandboxConfig's current ones, for a restore the request asks at data scope and also for a snapshot whose manifest
records data scope alone (a full capture uploaded as data), so only a restore that loads a checkpoint image runs the
recorded build. The on-node record a restore writes is that build, so the actor's next snapshot records it. atelet's
`RunResponse` and `RestoreResponse` carry `sandbox_assets` (field 10001), the binaries the actor runs on for the
atelet's architecture; the resume workflow records them on `ActorStatus.sandbox_assets` (field 10002, asset name, URL
and sha256, sorted by name; a report beyond the field's bounds is logged and left out) when the actor turns RUNNING.
`kubectl ate get actors` prints them as its last column, `SANDBOX`: the gVisor release path of the `gvisor` or `runsc`
asset's URL (`nightly/2026-09-09`), else the first asset with the head of its sha256. The demo e2e
`TestSandboxConfigChange` moves a per-test SandboxConfig from the 2026-09-02 nightly to the fixture's build while the
actor runs and proves the data-scope restore runs the new build and the full-scope one the old.

## Reason

A data-scope snapshot carries only the durable dirs and volumes and boots a fresh guest, so nothing ties it to the
build it was taken on; a restore that ran that build kept every later snapshot of the actor on it, and such actors
never reached the current runsc and kept the old release's bugs. Which build an actor runs was visible nowhere but on
the node.

## History

2026-11-08: written for giantswarm/substrate#175; the restore half rests on the line's own `restoreRuntime`, which
upstream does not have, so no upstream change is prepared.
