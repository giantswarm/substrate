---
id: ateapi-reads-1-8-actors-local-snapshot-node-assigned
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): read a 1.8 actor''s local-snapshot node and assigned node'
upstream:
  outcome: fork-only
  refs: []
  exit: 'folded at the conversion re-pin into the row conversion, which applies it before re-encoding; drops with that conversion at the following re-pin'
  class: 6
  destination: 'giantswarm/substrate ate-api row conversion of the conversion re-pin'
  first_step: 'at the conversion re-pin, the row conversion applies this migration before re-encoding (giantswarm/substrate)'
  source: 'giantswarm @ 87c54ce5f; conversion: new branch'
  test: 'the conversion lab upgrade from Substrate 1.16.3; the conversion counter reports no failed row'
  recheck: 'Substrate GA (agent-substrate/substrate#2004), fallback 2026-11-24, with the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: 'issuecomment-6301319662, amended issuecomment-6339634309'
  couplings: [compat-migrations]
  open_question: ''
depends_on: []
verified: 2026-10-21
---
# ate-api reads a 1.8 actor's local-snapshot node and assigned node

## Change

An Actor row is migrated on read (`cmd/ateapi/internal/store/atepg/legacy_snapshot.go`, `migrateLegacyLocalSnapshot` and `migrateLegacyAssignedNode`, beside the earlier migrations): a local snapshot takes its node from the 1.8 field 2 (`node_vms_with_local_snapshots`, a list holding the one node the pause ran on) into `node_name` and its content scope from the 1.8 field 3 into `content_scope` (field 2 since v0.5.0-alpha2); `ActorStatus.assigned_node` takes the worker assignment's node, else the local snapshot's. The rewritten row is persisted by the actor's next update; no SQL migration.

## Reason

v0.5.0-alpha2 renumbers `LocalSnapshot.content_scope` onto the field the 1.8 line used for its node list and decides where an actor lives by `ActorStatus.assigned_node`, which no 1.8 release wrote. Read as written, a paused 1.8 actor has no node to resume on and its DATA snapshot reads as FULL; a running one crashes on its first pause after the upgrade for an unknown node, and a delete or crash skips terminating its sandbox. Actors and their snapshots from a 1.8 release resume on the new base with no manual step.

## History

none: upstream's `AGENTS.md` keeps no compatibility with older formats before v1.0.0, so a migration from the line's 1.8 rows stays on the line. It leaves once no installation holds a row a 1.8 release wrote

the re-pin onto v0.5.0-alpha2 (`fix(ateapi): read a 1.8 actor's local-snapshot node and assigned node`)

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: fork-only (class 6): the next re-pin past kagent-dev/kagent 96c40300 is the first whose Substrate carries agent-substrate/substrate#2888; the fork converts state across it instead of cutting over; the row conversion applies this read-migration to every row before re-encoding it, so no legacy row survives, and the commit leaves at that re-pin.

2026-10-22: amended in the fork-reduction review of the compat-migrations theme: the conversion re-pin is the first past Substrate GA (fallback 2026-11-24), with one conversion written then against the frozen format; whether it runs as a one-shot agent-platform Job or as fork code for that one re-pin is decided then.
