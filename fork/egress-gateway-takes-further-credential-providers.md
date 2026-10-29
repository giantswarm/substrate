---
id: egress-gateway-takes-further-credential-providers
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(credprovider): serve k8s.io alone; the Google access-token provider is a plugin'
  - 'feat(credprovider): mint Google access tokens from SA key Secrets'
  - 'fix(chart): validate the egress gateway''s further credential providers'
  - 'feat(chart): further credential providers for the egress gateway'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2829, agent-substrate/substrate#2830, agent-substrate/substrate#3409, agent-substrate/substrate#3410, kagent-dev/substrate#90]
  exit: "kagent-dev/substrate#90 merged: drop; unreviewed on 2026-11-17: the platform chart overlays the egress gateway's credentialProviders entries and the file drops"
depends_on: []
verified: 2026-10-29
---
# The egress gateway takes further credential providers

## Change

The egress gateway takes further credential providers (`credentialProvider.additionalProviders`: `uriAuthority`, a lowercase DNS name, and `host`, `<service>.<namespace>.svc:<port>`, rendered on both egress listeners beside the bundled `k8s.io` provider, dialed with the gateway's pod identity and verified against the `servicedns.podcert.ate.dev` trust bundle; an entry cannot replace `k8s.io` or repeat an authority; `charts/substrate/tests/atenet_egress_credential_providers_test.yaml`)

## Reason

kagent injects the caller of the current turn at egress through its own provider, so the caller's bearer never enters the actor: agentgateway already selects a provider by URI authority and fetches the actor's egress policy on every request, but the chart rendered one provider only

## History

[kagent-dev/substrate#90](https://github.com/kagent-dev/substrate/pull/90) (open, 2026-10-18, the chart); [agent-substrate/substrate#2829](https://github.com/agent-substrate/substrate/issues/2829) (open, 2026-10-18) and [#2830](https://github.com/agent-substrate/substrate/pull/2830) (draft, 2026-10-18) for ate-setup

[#248](https://github.com/giantswarm/substrate/pull/248) (carries [#104](https://github.com/giantswarm/substrate/pull/104) onto the v0.4.0-alpha1 line)

2026-10-20, decided on internal issue 38652, comment 6301541135: class 5 upstream PR in flight, fallback class 3 platform; destination kagent-dev/substrate#90 (the chart) and agent-substrate/substrate#2830 (ate-setup, for #2829); the dataplane already selects a provider by URI authority; first step none, both are filed; branch the `giantswarm` commit of this file; test the chart's additional-providers cases; depends on theme T6; re-check 2026-11-17; owner unassigned; draft: internal issue 38652, comment 6301544198.

2026-10-29: agent-substrate/substrate#3409 merged on 2026-10-29 as `de4c5d9b0` and routes several credential providers through ate-setup and atenet, on the Envoy dataplane too, so #2830 was closed as superseded; [#3410](https://github.com/agent-substrate/substrate/pull/3410) (open) documents it. This file carries only the chart half, so the exit waits on kagent-dev/substrate#90 alone.
