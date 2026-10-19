---
id: egress-gateway-counts-refusals-by-template-and-reason
fork: substrate
theme: new-rpcs
category: feature
status: carried
commits:
  - 'feat(atenet): count egress refusals by actor template and reason'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2527]
  exit: as#2527 merged
depends_on: [podmonitors-six-workloads-that-serve-metrics]
verified: 2026-10-19
---
# The egress gateway counts its refusals by actor template and reason

## Change

The egress ext_proc handler counts every request it refuses on `atenet.router.egress.refused` (`{request}`; Prometheus `atenet_router_egress_refused_total` on the egress pod's `metrics` port, which the chart's `atenet-egress` PodMonitor already scrapes), labeled `ate.template.atespace`, `ate.template.name` (`unknown` when the gateway does not know the actor) and `ate.egress.refusal.reason`: `unauthenticated`, `unknown_actor`, `actor_not_running`, `no_policy`, `not_in_policy`, `bad_destination`, `credential`, `unavailable` (`websocket` exists upstream only). The template comes from the `GetActor` the CONNECT makes and is remembered per actor for the requests in its tunnel; a callout whose caller gave up is not counted (`cmd/atenet/internal/router/egress/metrics.go`, `internal/ateattr`, the registry in `docs/metrics/registry/metrics.yaml`, `docs/observability.md`). Unit tests count each refusal path once with its labels.

## Reason

The only metric of an egress refusal was the gateway dataplane's request counter, which names no subject. An alert on egress refusals per template, the kill switch for coding agents that something steers to send data out, needs the refusals counted per template and reason. The actor and the destination are not labels (the metric registry's no-actor-identity rule); they stay on the refusal's log line.

## History

[agent-substrate/substrate#2527](https://github.com/agent-substrate/substrate/pull/2527), open, from the branch `upstream/egress-refusal-counter` on upstream `main` @ `9e7d01ef`

[#351](https://github.com/giantswarm/substrate/pull/351): the fork's cherry-pick of the upstream commit, resolved against the line's `validateActor` (RUNNING or RESUMING) and `handleRequest`, which has no WebSocket refusal
