---
id: chart-adds-podlabels-every-long-running-pod
fork: substrate
theme: chart-knobs
category: gs-specific
status: carried
commits:
  - 'feat(chart): podLabels on the chart''s pods'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#48]
  exit: kd#48 merged
depends_on: []
verified: 2026-10-19
---
# The chart adds `podLabels` to every long-running pod, next to its own `app` label, which a …

## Change

The chart adds `podLabels` to every long-running pod, next to its own `app` label, which a `podLabels` entry cannot replace (the render fails; the `rustfs-bucket-init` Job is left out, its pod template is immutable)

## Reason

the collector the platform exports OTLP to takes the tenant from the pod label `observability.giantswarm.io/tenant` when an export carries no header, and the chart has neither a pod-label nor an OTLP-headers key: every Substrate span, metric and log reached the collector without a tenant and was dropped

## History

[kagent-dev/substrate#48](https://github.com/kagent-dev/substrate/pull/48), **open**, carried until it merges and the next re-pin drops it

`628377d7` ([#55](https://github.com/giantswarm/substrate/pull/55))
