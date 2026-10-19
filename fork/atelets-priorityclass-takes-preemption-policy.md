---
id: atelets-priorityclass-takes-preemption-policy
fork: substrate
theme: chart-knobs
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#247]
  exit: kd chart PR
depends_on: []
commit: 768e18ed
verified: 2026-10-19
---
# atelet's PriorityClass takes its preemption policy

- **Status:** carried
- **Upstream:** to prepare: the patch is upstream-shaped (a knob, default unchanged); no upstream item (agent-substrate/substrate#247 added `priorityClassName` for workers only)
- **Our commit:** `768e18ed`, [#87](https://github.com/giantswarm/substrate/pull/87)

## Change

atelet's PriorityClass takes its preemption policy (`atelet.priorityClass.preemptionPolicy`: `PreemptLowerPriority`, the default and upstream's behaviour, or `Never`; the non-preempting class is `ate-node-critical-non-preempting`, because a PriorityClass's `preemptionPolicy` is immutable)

## Reason

a first install over full nodes let the atelet DaemonSet evict the cluster's own workloads (monitoring, admission webhooks), which paged the installation; with `Never` atelet keeps its priority but waits for room ([giantswarm/agent-platform#731](https://github.com/giantswarm/agent-platform/issues/731))
