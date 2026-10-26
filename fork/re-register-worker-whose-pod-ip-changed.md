---
id: re-register-worker-whose-pod-ip-changed
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'workersync: re-register a worker whose pod IP changed in place'
upstream:
  outcome: superseded
  refs: [agent-substrate/substrate#1837, agent-substrate/substrate#1983, agent-substrate/substrate#944, agent-substrate/substrate#2988]
  exit: "at the re-pin onto a commit containing 06737b8f3 (as#2988): check that a worker whose pod IP changed in place re-registers, then drop"
depends_on: [ateom-keeps-re-asserting-capacity-report]
verified: 2026-10-26
---
# Re-register a worker whose pod IP changed in place

## Change

Re-register a worker whose pod IP changed in place (the Syncer's `createOrUpdateWorker` treats an IP disagreement as a new ateom in the same pod: `reconcileDeadWorker` — the DeleteWorker workflow, NOT_FOUND = success — then `registerWorker`; the Worker's `ip` stays immutable, a new server uid replaces the dead record)

## Reason

a pod's IP changes only when its sandbox is recreated — a node reboot, a kubelet or container-runtime restart, a host resume — which takes the ateom and every actor it hosted down while the pod keeps its UID and so its Worker; the Syncer only logged `registered worker IP disagrees with its pod`, the router kept sending the assigned actors' requests to the dead `worker_pod_ip`, each timed out, and an actor caught mid-resume stayed `RESUMING`, blocking its suspend and delete until the worker pods were rolled by hand (the second half of the lab's hanging turns after a host resume)

## History

to file: the upstream-shaped patch is branch [`upstream/workersync-pod-ip-repair`](https://github.com/giantswarm/substrate/tree/upstream/workersync-pod-ip-repair) here (`13e574e0`, on the mirror `main` @ `007eb1ee`; its message is the pull-request text). agent-substrate/substrate#1983 (open draft, 2026-09-29, fixes #1837) updates the worker IP on a pod IP change, the same fix: it is reviewed there and this patch drops when it merges. Composes with agent-substrate/substrate#944 (per-family worker addresses for dual-stack — an issue, not a pull request): the Worker's shape and its immutability rule are unchanged. A team member opens the pull request with DCO sign-off once the team has reviewed it. At the 2026-10-08 re-pin onto v0.4.0-alpha1: the Worker's address is `ips` (repeated, dual-stack) and carries an `epoch`; upstream still only logs an IP disagreement, so the patch compares `ips` with the pod's `PodIPs` and registers the new record with both fields.

`b438d13c`, [#20](https://github.com/giantswarm/substrate/pull/20) (rebase-merged 2026-09-12)

2026-10-20: agent-substrate/substrate#944 was closed as completed on 2026-10-01 by #2052 (`8a6b5e14`, in the pin, the `ips` field the patch already compares), which is not this fix, so the outcome stays open on #1983.

2026-10-26: agent-substrate/substrate#1983 was closed on 2026-10-22 as fixed instead by #2988 (`06737b8f3`, worker restarts detected by an epoch the worker registers), and #1837 was closed as completed the same day. Whether the epoch covers an in-place IP change without a restart is unchecked, so the exit is a check at the re-pin, not a blind drop.
