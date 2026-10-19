---
id: make-every-pause-durable-after-pauseactor-finalizes
fork: substrate
theme: durable-pause
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#398, agent-substrate/substrate#401, agent-substrate/substrate#798]
  exit: design issue upstream within 4 weeks or converge on as#798
depends_on: [durable-pauses-background-commit-holds-no-lease, externalsnapshot-source-local-snapshot-name-durable-pauses, lines-own-postgresql-migrations-live-cmd-ateapi]
commit: 012fabd0
verified: 2026-10-19
---
# Make every pause durable

- **Status:** carried
- **Upstream:** prepared in the fork: branch [`upstream/durable-pause`](https://github.com/giantswarm/substrate/tree/upstream/durable-pause) (mirror `main` @ `463f483c` + the patch, `3f16aaaa`, DCO-signed, no fork references, without the carried lease-context semantics; its message is the pull-request text; its base predates upstream's `RevertActor`, so it still records the copy as `external_snapshot`: the rebase onto upstream `main` for sending takes `LocalSnapshotInfo.durable_copy` from the fork line). agent-substrate/substrate#398 names the node-gone case and defers this remediation to a snapshot-model redesign nobody has opened; #401 (open) is the error message only; agent-substrate/substrate#798 (open) proposes opt-in durability instead of pause vs suspend, the design alternative. To be sent once the team has reviewed the list
- **Our commit:** `012fabd0` ([#34](https://github.com/giantswarm/substrate/pull/34), rebase-merged 2026-09-15; `cmd/ateapi/internal/controlapi/durable_pause.go`, `workflow_{pause,resume,suspend,delete}.go`, `scheduling.go`, `cmd/atelet/main.go`, `ateapi.proto` `ExternalSnapshot.source_local_snapshot_name` and `LocalSnapshotInfo.durable_copy` (the copy moved out of `external_snapshot`, where it had replaced and released the suspend snapshot a revert from PAUSED returns to: upstream's `RevertActor` then reverted to the pause, the e2e `RevertResumeActor` failed whenever the upload beat the revert, [#89](https://github.com/giantswarm/substrate/issues/89)), `atelet.proto` `UploadPausedCheckpointRequest.keep_local` + `PruneLocalCheckpoints`; first released as `v0.0.30-gs.3`); also [#100](https://github.com/giantswarm/substrate/pull/100) (`test(ateapi): compare the pause records without the background copy`), [#90](https://github.com/giantswarm/substrate/pull/90) (`fix(ateapi): keep the durable pause copy out of external_snapshot`)

## Change

Make every pause durable: after `PauseActor` finalizes, ate-api uploads the pause snapshot in the background through the atelet path a paused-origin suspend uses (`UploadPausedCheckpointRequest.keep_local`), records the copy as `local_snapshot_info.durable_copy` (its `source_local_snapshot_name` = the pause snapshot), leaving `external_snapshot` the last suspend's so `RevertActor` still returns to it; the copy goes with the pause: released when the next pause replaces it, a revert discards the pause or a suspend uploads a snapshot of its own, committed as the external snapshot by a suspend that adopts it; and a resync every minute finds pauses without a copy; once the copy exists the snapshot's nodes are a placement preference (`scheduling.Constraints.PreferredNodes`) — local restore on the node, the copy (captured scope, memory included) anywhere else; an earlier suspend's snapshot never stands in (`durablePauseCopy`); a suspend of such an actor adopts the copy without dialing the node (the local copy pruned best-effort through the new atelet `PruneLocalCheckpoints`), delete collects both copies; the copy is committed under the actor's lease for two store round trips and `acquireActorLease` retries a held lease for about a second before `Aborted`

## Reason

a session kagent parks on `input-required`/`auth-required` is paused, not suspended: its only snapshot lived on the worker's node and went with it on a spot interruption or a node roll — the next message failed after 60 s as `ResourceExhausted: no free workers available` for an idle pool and the actor could neither resume nor be suspended or deleted (2026-09-15, the durable-pause half — the fail-fast half is #35). Proven in a two-worker agentlab: the copy lands 60–280 ms after the pause checkpoint; with the pause's node stopped and deleted, the parked turn continues on the other node 4.6–13 s after the approval; an actor paused on the dead node is deletable; the fork's e2e suites caught two lease races before the final cut
