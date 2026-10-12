# A signal without an OTLP endpoint exports nothing

- **Status:** carried
- **Upstream:** upstream-ready (upstream `main`'s `_helpers.tpl` turns off only a disabled signal); queued for review before it goes upstream
- **Our commit:** [#136](https://github.com/giantswarm/substrate/pull/136)

## Change

A signal without an OTLP endpoint exports nothing (`substrate.otel.env` sets `OTEL_<SIGNAL>_EXPORTER=none` for every signal that resolves no endpoint, neither `otel.endpoint` nor `otel.<signal>.endpoint`, as it did for a disabled one; chart unit tests for the default, a per-signal and the shared endpoint)

## Reason

the signals are enabled by default with no endpoint, so the chart rendered no `OTEL_*` variable and every Go component's SDK exported traces and metrics to the default `localhost:4317`: on a cluster without a collector the credential provider and the egress ext-proc logged a refused export once a minute ([#123](https://github.com/giantswarm/substrate/issues/123)). The committed manifests render with an endpoint and stay unchanged; a release with the default values rolls its Go components once
