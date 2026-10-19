---
id: fail-paused-actors-resume-fast-when-node
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1761, agent-substrate/substrate#397, agent-substrate/substrate#398, agent-substrate/substrate#401, agent-substrate/substrate#589]
  exit: upstream PR
depends_on: [make-every-pause-durable-after-pauseactor-finalizes]
commit: 7ef4058d
verified: 2026-10-19
---
# Fail a `PAUSED` actor's resume fast when the node holding its local snapshot is gone, and crash it …

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/pause-node-gone`](https://github.com/giantswarm/substrate/tree/upstream/pause-node-gone) here (`f9d1cdc1`, on the mirror `main` @ `463f483c` = v0.0.29; its message is the pull-request text). agent-substrate/substrate#398 names the case and PR agent-substrate/substrate#401 (open since 2026-07-07, written for a scheduler shape that no longer exists) improves the message only and keeps `FailedPrecondition`, which the dataplane parks on; #397 is the empty-node variant whose write side upstream already refuses — this patch carries #401's classification into the current scheduler, adds the Node-informer judgement, the crash and the read-side guard; agent-substrate/substrate#589 and #1761 (part 1) report the same node-pinning loss. A team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it
- **Our commit:** `7ef4058d`, [#35](https://github.com/giantswarm/substrate/pull/35) (rebase-merged 2026-09-15; first release v0.0.30-gs.2). Proven in a two-worker agentlab on gs.2: the worker node stopped and deleted, the answer refused in 82 ms (`runtime lost: Actor … crashed` through kagent), the actor `CRASHED`, deleted in 30 ms; a direct suspend of a second such actor crashed it in 62 ms; a session paused on the surviving node resumed there

## Change

Fail a `PAUSED` actor's resume fast when the node holding its local snapshot is gone, and crash it when no durable snapshot remains (`scheduling.Schedule` returns a `NoCapacityError` under a node restriction — the restricted nodes, the eligible workers on them, the ACTIVE workers of any pool that report them; `AssignWorker` answers `ResourceExhausted` naming the node while eligible workers are busy or the node is present without one, and, once the node is gone — no ACTIVE worker reports it and its Node object is absent, read from a Node informer ate-api-server now runs (RBAC `nodes` get/list/watch) — `DataLoss`: the actor without an `external_snapshot` crashed with `LOCAL_SNAPSHOT_GONE` and the crash directive, one with a durable snapshot left as it is; the paused-origin suspend takes the same exit; the atenet router maps `DataLoss` to `410` with the cause; empty entries in `node_vms_with_local_snapshots` are ignored on read)

## Reason

a session kagent parks on `input-required` is paused, not suspended: its only snapshot lives on the worker's node; a spot interruption or consolidation deletes it, and the next message waited out the dataplane's whole parking budget (30 s, twice per turn) on `ResourceExhausted "no free workers available"` — the message of a full pool for an idle one — while the actor stayed `PAUSED` for good: a suspend dialled the atelet on the dead node forever, so a delete that suspends first failed too (2026-09-15, one session; one more). The worker scan alone does not decide "gone": a pool roll, an ateom restart or an atelet DaemonSet update leave a node without an ACTIVE worker for a while with the snapshot intact on its disk. The branch that keeps a durable snapshot is restored from it by the durable-pause patch (the row below)
