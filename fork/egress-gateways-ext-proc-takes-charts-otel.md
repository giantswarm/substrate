---
id: egress-gateways-ext-proc-takes-charts-otel
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'fix(helm): give the egress ext-proc the chart''s otel settings'
upstream:
  outcome: to-file
  refs: [kagent-dev/substrate#46, kagent-dev/substrate#48]
  exit: kd chart PR
depends_on: [chart-adds-podlabels-every-long-running-pod, podmonitors-six-workloads-that-serve-metrics]
verified: 2026-10-19
---
# The egress gateway's ext-proc takes the chart's `otel` settings like every other Go component

## Change

The egress gateway's ext-proc takes the chart's `otel` settings like every other Go component (`substrate.otel.env` and `OTEL_RESOURCE_ATTRIBUTES` on the `atenet-egress` `ext-proc` container; a chart unit test)

## Reason

the container rendered no `OTEL_*` variable, so its SDK exported traces and metrics to the default `localhost:4317` whatever `otel` said: a cluster without a collector logged `OpenTelemetry SDK error … dial tcp [::1]:4317` once a minute, and with one its spans never reached it ([giantswarm/agent-platform#463](https://github.com/giantswarm/agent-platform/issues/463))

## History

upstream-ready for [kagent-dev/substrate](https://github.com/kagent-dev/substrate), which ships the chart (its `charts/substrate/templates/atenet-egress.yaml` has the same gap); waits for a free slot there, with kagent-dev/substrate#46 and #48 open

`126a8682` ([#106](https://github.com/giantswarm/substrate/pull/106))
