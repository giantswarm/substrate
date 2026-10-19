---
id: podmonitors-six-workloads-that-serve-metrics
fork: substrate
theme: chart-knobs
category: feature
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1781, kagent-dev/substrate#46]
  exit: kd#46 merged
depends_on: []
commit: 3fb1622a
verified: 2026-10-19
---
# PodMonitors for the six workloads that serve metrics

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#46](https://github.com/kagent-dev/substrate/pull/46) — **open**, carried until it merges and the next re-pin drops it. `kagent-dev/substrate` is the upstream for a chart change: `agent-substrate/substrate` packages no chart at all, and deploys from `manifests/ate-install/` with Kustomize
- **Our commit:** `3fb1622a`

## Change

PodMonitors for the six workloads that serve metrics (`metrics.podMonitor`, off by default; one object per workload, each pinned to the port name its own pod declares and, for the credential provider, to the `substrate.fullname` label)

## Reason

the fleet's `alloy-metrics` discovers ServiceMonitors, PodMonitors and ScrapeConfigs only — `observability-operator`'s metrics collector configures no `discovery.kubernetes`, so the `prometheus.io/scrape` annotations three of these workloads carry are ignored and the whole ate metric registry is served and thrown away: actor crashes, lifecycle and scheduler durations, checkpoint and restore phases, actor CPU and memory, image-cache hits, worker pool desired against ready, router route duration and parking, and `ate-controller`'s `controller_runtime_*` and `workqueue_*`. The platform enables them with the tenant label
