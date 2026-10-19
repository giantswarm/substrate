---
id: egress-gateways-agentgateway-config-has-no-protocol
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: fork-only
  refs: [giantswarm/substrate#120]
  exit: carried on release-1.3 only; no commit on giantswarm, dropped from this ledger once release-1.3 is retired
depends_on: []
commit: giantswarm/substrate#141
verified: 2026-10-19
---
# The egress gateway's agentgateway config has no `protocol

- **Status:** carried
- **Upstream:** fork-only: the 1.4 line runs agentgateway 2.3.0, whose `proxy_tcp` drops an actor stream that is not in TLS-passthrough mode ([#120](https://github.com/giantswarm/substrate/issues/120)); upstream's chart keeps the listener next to its `TLS` passthrough listener
- **Our commit:** `release-1.3`: [#141](https://github.com/giantswarm/substrate/pull/141) (first release 1.3.2)

## Change

The egress gateway's agentgateway config has no `protocol: TCP` listener (the dynamic-target `tcpRoutes` listener is removed from `atenet-egress.yaml`; `charts/substrate/tests/atenet_egress_test.yaml` asserts the rendered config has no raw-stream listener)

## Reason

agentgateway 2.2.1's `proxy_tcp` has no actor guard: a tunnel whose first bytes were neither a TLS ClientHello nor an HTTP request fell through the internal `AUTO` bind to that listener, which dialed the CONNECT authority with no `substrateEgress` policy, so an actor's non-HTTP TCP reached any destination, with or without an egress policy ([giantswarm/substrate#140](https://github.com/giantswarm/substrate/issues/140)). Without the listener the bind closes the stream; this line's policy API has no TLS-passthrough rule that could select one
