# The egress gateway's ext-proc names its telemetry `atenet-egress`

- **Status:** carried
- **Upstream:** fork-only chart change; upstream's `atenet-egress.yaml` has the same gap, not prepared for upstream
- **Our commit:** `6627a636` ([#217](https://github.com/giantswarm/substrate/pull/217), rebase-merged 2026-10-09; first release 1.6.2)

## Change

The egress gateway's ext-proc names its telemetry `atenet-egress` (`OTEL_SERVICE_NAME` on the `atenet-egress` `ext-proc` container; a chart unit test)

## Reason

the `atenet` binary hardcodes the service name `atenet-router` for both modes and the container set none, so egress spans and logs carried `service.name=atenet-router`
