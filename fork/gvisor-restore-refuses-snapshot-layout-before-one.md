---
id: gvisor-restore-refuses-snapshot-layout-before-one
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateom): refuse a restore from the pre-per-volume durable layout'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2462]
  exit: as#2462 merged
depends_on: []
verified: 2026-10-19
---
# A gVisor restore refuses a snapshot in the layout before one tar per durable volume

## Change

A gVisor restore refuses a snapshot in the layout before one tar per durable volume (`durable-dir.tar` in the restore directory) with `FailedPrecondition` "snapshot written before the per-volume layout; re-snapshot from a running actor or reset the golden snapshot", before it extracts anything (`cmd/ateom-gvisor/durable.go`)

## Reason

since the one-tar-per-volume layout (agent-substrate/substrate#2110, two rows up) a restore skips a missing `durable-dir-<volume>.tar` as a volume added since the snapshot, so a snapshot written before it restored with no error and an empty `/data`: the Go ADK failed with `no such table: app_states`, the claude Harness with `Harness runtime execution failed`, and a missed snapshot reset lost the sessions' data without a word ([#263](https://github.com/giantswarm/substrate/issues/263)). Now the Resume fails and the actor is CRASHED with that message

## History

[agent-substrate/substrate#2462](https://github.com/agent-substrate/substrate/pull/2462) (ours, open; both ateoms). Falls away at the re-pin onto the first release that carries it

[#265](https://github.com/giantswarm/substrate/pull/265): `git cherry-pick -x` of `11a6bc4f`, adapted to the line: the micro-VM hunks are left out, its ateom wrote and read the single `durable-dir.tar` before the pin; v0.5.0-alpha2 carries agent-substrate/substrate#2283, the micro-VM ateom's one tar per volume, and the line still carries only the gVisor half
