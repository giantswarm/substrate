---
id: mark-busy-worker-pods
fork: substrate
theme: session-lifecycle
category: feature
status: carried
commits:
  - 'feat(atecontroller): mark worker pods hosting actors for autoscalers'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-19
---
# Mark the worker pods that host actors for the autoscaler

## Change

ate-controller's `BusyMarker` (`cmd/atecontroller/internal/workersync/busy.go`) lists the Workers every `--busy-worker-sync-period` (5s) and applies the annotations of `--busy-worker-annotations` plus the label `ate.dev/busy=true` to the pod of every Worker that reports an allocated actor, and releases them once it hosts none (server-side apply, field manager `ate-controller-busy-worker`, preconditioned on the pod UID; terminating pods are skipped). ate-controller's ClusterRole gains `patch` on pods. The chart's `ateController.busyWorker.annotations` (default `{}`, off) and `ateController.busyWorker.syncPeriod` set the flags; differs from the upstream branch only where the line's chart already carries `ateController.gvisorWorker`.

## Reason

A worker pod's SIGTERM stops every actor it hosts, so Karpenter's consolidation of a node lost the turns in flight on its workers (giantswarm/agent-platform#1014). A static `karpenter.sh/do-not-disrupt` in the pool template would also pin idle workers' nodes forever; marking only busy workers keeps scale-down working.

## History

to file: the upstream-shaped patch is branch [`upstream/busy-worker-do-not-disrupt`](https://github.com/giantswarm/substrate/tree/upstream/busy-worker-do-not-disrupt) here (`3913e1de`, on the mirror `main` @ `5cf1c245`; its message is the pull-request text). A team member opens the pull request with DCO sign-off once the team has reviewed it.

[#425](https://github.com/giantswarm/substrate/pull/425)
