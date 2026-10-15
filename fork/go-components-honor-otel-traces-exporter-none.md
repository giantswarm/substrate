# The Go components honor `OTEL_TRACES_EXPORTER=none`

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2189](https://github.com/agent-substrate/substrate/pull/2189) (draft, with the #1878 follow-up and the ateom row below; the upstream version also warns on an unsupported value), carried until it merges. At the 2026-10-08 re-pin onto v0.4.0-alpha1: rewritten on upstream's `metricsPushEnabled` shape as `tracesPushEnabled(ctx)` (`otlp`, the default, or `none`; an unrecognized value keeps the export and warns), so the line and agent-substrate/substrate#2189 now agree. At the 2026-10-12 re-pin onto v0.5.0-alpha2: the `serverboot` half is upstream's (agent-substrate/substrate#2193), and the line keeps only the chart's values and README rows.
- **Our commit:** `48560a41` ([#107](https://github.com/giantswarm/substrate/pull/107))

## Change

The Go components honor `OTEL_TRACES_EXPORTER=none` (the chart's `otel.traces.enabled: false`): `serverboot.InitTracing` builds no OTLP span exporter and keeps the provider, its sampler and the TraceContext propagator

## Reason

the chart rendered the variable and nothing read it, so ate-api-server, ate-controller, atelet, the atenet router and ateom exported spans to the SDK default `localhost:4317` on a cluster without a collector and logged `OpenTelemetry SDK error … traces export … dial tcp [::1]:4317` on every batch ([giantswarm/agent-platform#463](https://github.com/giantswarm/agent-platform/issues/463)); the traces counterpart of the `OTEL_METRICS_EXPORTER=none` row
