---
id: kubernetes-credential-provider-takes-charts-otel-settings
fork: substrate
theme: chart-knobs
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/substrate#46, kagent-dev/substrate#48]
  exit: kd chart PR
depends_on: [podmonitors-six-workloads-that-serve-metrics]
commit: f5d3b0d0
verified: 2026-10-19
---
# The Kubernetes credential provider takes the chart's `otel` settings like every other Go component

- **Status:** carried
- **Upstream:** upstream-ready for [kagent-dev/substrate](https://github.com/kagent-dev/substrate), which ships the chart (its `charts/substrate/templates/k8s-credential-provider.yaml` has the same gap); waits for a free slot there, with kagent-dev/substrate#46 and #48 open
- **Our commit:** `f5d3b0d0` ([#116](https://github.com/giantswarm/substrate/pull/116))

## Change

The Kubernetes credential provider takes the chart's `otel` settings like every other Go component (`substrate.otel.env` and `OTEL_RESOURCE_ATTRIBUTES` on the `k8s-credential-provider` container; a chart unit test)

## Reason

the container rendered no environment at all, so its SDK pushed metrics to the default `localhost:4317` whatever `otel` said: a cluster without a collector logged `failed to upload metrics … dial tcp [::1]:4317` once a minute, and with one its telemetry never reached it ([giantswarm/agent-platform#463](https://github.com/giantswarm/agent-platform/issues/463)); the provider twin of the egress `ext-proc` row
