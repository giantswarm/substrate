---
id: workerpools-ateom-exports-nothing-when-ate-controller
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(atecontroller): turn ateom''s exporters off without an endpoint'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2189]
  exit: as#2189 merged
depends_on: [go-components-honor-otel-traces-exporter-none]
verified: 2026-10-19
---
# A WorkerPool's ateom exports nothing when ate-controller has no OTLP endpoint

## Change

A WorkerPool's ateom exports nothing when ate-controller has no OTLP endpoint (`OTEL_TRACES_EXPORTER=none` and `OTEL_METRICS_EXPORTER=none` on the `ateom` container, where it rendered no OTEL variable)

## Reason

the worker Deployment's comment promised "empty disables ateom telemetry entirely", but with no variable the SDK exported to `localhost:4317`, or through a relay socket a previous atelet left on the node: on a cluster without a collector every worker logged `OpenTelemetry SDK error … failed to upload metrics` once a minute ([giantswarm/agent-platform#463](https://github.com/giantswarm/agent-platform/issues/463)). The traces half needs the `OTEL_TRACES_EXPORTER=none` row. Adding the two variables rolls the worker pods once on a cluster without an endpoint; one with an endpoint renders unchanged

## History

[agent-substrate/substrate#2189](https://github.com/agent-substrate/substrate/pull/2189) (open, not a draft, reviewed by JeffLuoo on 2026-10-13; with the row above), carried until it merges

`fb79b373` ([#108](https://github.com/giantswarm/substrate/pull/108))
