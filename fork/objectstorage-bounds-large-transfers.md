---
id: objectstorage-bounds-large-transfers
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2538]
  exit: as#2538 merged
depends_on: []
commit: giantswarm/substrate#362
verified: 2026-10-19
---
# objectstorage bounds the large transfers a process runs at once

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2538](https://github.com/agent-substrate/substrate/pull/2538) (ours, open), the same commit on upstream `main` @ `29bebd3a`. Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#362](https://github.com/giantswarm/substrate/pull/362)

## Change

At most two large transfers run at once in a process (`pkg/objectstorage/largetransfer.go`); the rest wait for a slot or for their context to end. An upload counts when its file is larger than one part; a ranged download takes its slot before it allocates its buffers and gives it back on `Close`. Objects of one part or less never wait.

## Reason

Each large upload holds about 200 MiB (the parallel compressor's chunks plus up to five 32 MiB parts) and each ranged download 128 MiB, one per actor suspending or restoring at the same time, so atelet's memory grew with their number. Since the v0.5.0-alpha2 pin the chart limits atelet to 1 GiB: a handful of micro-VM actors checkpointing together OOM-killed atelet and failed every checkpoint and restore it had in flight, which kept helm-e2e's micro-VM lane red ([#357](https://github.com/giantswarm/substrate/issues/357)). More concurrent transfers added memory and no throughput: each one already uses every core and several connections.
