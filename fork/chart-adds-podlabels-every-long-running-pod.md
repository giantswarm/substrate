# The chart adds `podLabels` to every long-running pod, next to its own `app` label, which a …

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#48](https://github.com/kagent-dev/substrate/pull/48), **open**, carried until it merges and the next re-pin drops it
- **Our commit:** `628377d7` ([#55](https://github.com/giantswarm/substrate/pull/55))

## Change

The chart adds `podLabels` to every long-running pod, next to its own `app` label, which a `podLabels` entry cannot replace (the render fails; the `rustfs-bucket-init` Job is left out, its pod template is immutable)

## Reason

the collector the platform exports OTLP to takes the tenant from the pod label `observability.giantswarm.io/tenant` when an export carries no header, and the chart has neither a pod-label nor an OTLP-headers key: every Substrate span, metric and log reached the collector without a tenant and was dropped
