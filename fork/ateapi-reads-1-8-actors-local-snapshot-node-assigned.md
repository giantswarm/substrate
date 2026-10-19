---
id: ateapi-reads-1-8-actors-local-snapshot-node-assigned
fork: substrate
theme: compat-migrations
category: compat
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: line release after which no 1.8 rows remain
depends_on: []
commit: ""
verified: 2026-10-19
---
# ate-api reads a 1.8 actor's local-snapshot node and assigned node

- **Status:** carried
- **Upstream:** none: upstream's `AGENTS.md` keeps no compatibility with older formats before v1.0.0, so a migration from the line's 1.8 rows stays on the line. It leaves once no installation holds a row a 1.8 release wrote
- **Our commit:** the re-pin onto v0.5.0-alpha2 (`fix(ateapi): read a 1.8 actor's local-snapshot node and assigned node`)

## Change

An Actor row is migrated on read (`cmd/ateapi/internal/store/atepg/legacy_snapshot.go`, `migrateLegacyLocalSnapshot` and `migrateLegacyAssignedNode`, beside the earlier migrations): a local snapshot takes its node from the 1.8 field 2 (`node_vms_with_local_snapshots`, a list holding the one node the pause ran on) into `node_name` and its content scope from the 1.8 field 3 into `content_scope` (field 2 since v0.5.0-alpha2); `ActorStatus.assigned_node` takes the worker assignment's node, else the local snapshot's. The rewritten row is persisted by the actor's next update; no SQL migration.

## Reason

v0.5.0-alpha2 renumbers `LocalSnapshot.content_scope` onto the field the 1.8 line used for its node list and decides where an actor lives by `ActorStatus.assigned_node`, which no 1.8 release wrote. Read as written, a paused 1.8 actor has no node to resume on and its DATA snapshot reads as FULL; a running one crashes on its first pause after the upgrade for an unknown node, and a delete or crash skips terminating its sandbox. Actors and their snapshots from a 1.8 release resume on the new base with no manual step.
