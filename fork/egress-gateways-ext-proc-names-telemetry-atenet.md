---
id: egress-gateways-ext-proc-names-telemetry-atenet
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(chart): name the egress ext-proc''s telemetry atenet-egress'
upstream:
  outcome: fork-only
  refs: []
  exit: trivial kd chart PR
depends_on: []
verified: 2026-10-19
---
# The egress gateway's ext-proc names its telemetry `atenet-egress`

## Change

The egress gateway's ext-proc names its telemetry `atenet-egress` (`OTEL_SERVICE_NAME` on the `atenet-egress` `ext-proc` container; a chart unit test)

## Reason

the `atenet` binary hardcodes the service name `atenet-router` for both modes and the container set none, so egress spans and logs carried `service.name=atenet-router`

## History

fork-only chart change; upstream's `atenet-egress.yaml` has the same gap, not prepared for upstream

`6627a636` ([#217](https://github.com/giantswarm/substrate/pull/217), rebase-merged 2026-10-09; first release 1.6.2)
