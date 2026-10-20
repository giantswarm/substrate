---
id: egress-gateway-mints-and-injects-actor-jwts
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - Egress policies can inject actor JWTs
  - 'feat(egress): run the agentgateway line''s stable 2.4.0'
  - Rename `MintActorJWTRequest.audience` to `audiences`.
  - 'feat(egress): inject actor JWTs on the agentgateway dataplane with the agentgateway line''s 2.4.0-rc.1'
  - 'test(chart): admit a release candidate of the agentgateway line as the dataplane image'
  - 'feat(egress): run the agentgateway line''s 2.4.0-rc.2, which mints through MintActorJWT'
  - Egress gateway can mint and cache actor JWTs
  - 'feat(ateapi): serve the actor JWT mint as Control.MintActorJwt, upstream''s name'
upstream:
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#2135, agent-substrate/substrate#2141, agent-substrate/substrate#2421, agent-substrate/substrate#2520]
  exit: next re-pin
depends_on: [giantswarm/agentgateway-upstream:the-substrate-egress-injects-a-minted-actor-jwt]
verified: 2026-10-20
---
# The egress gateway mints and injects actor JWTs

## Change

The Envoy ext_proc gateway (`cmd/atenet/internal/router/egress`) injects an `actor_jwt` entry of `replace_headers` whose header the request carries. It writes the entry's prefix plus a JWT minted through `Control.MintActorJwt` for the actor behind the tunnel, with the entry's audiences and lifetime. The `actorjwt` package caches a token per actor, sorted audiences and lifetime until a third of its lifetime remains, and concurrent misses share one mint. A failed mint denies the request: 403 when the actor is gone, 503 when ate-api is unavailable or the mint timed out, 500 otherwise. Actor JWTs need no credential provider. `MintActorJwtRequest.audience` is `audiences` (same field number). In the replay of #2141, `applyEffects` keeps the line's `ReplaceHeadersFor(effects, path)`, and upstream's `providerCredential` keeps the line's provider-named denials (`mapCredentialProviderError(provider, …)`, `credentialProviderFailed`). The agentgateway dataplane, which the line runs, makes the same injection from the agentgateway line's `2.4.0` on ([giantswarm/agentgateway-upstream#80](https://github.com/giantswarm/agentgateway-upstream/pull/80), [#82](https://github.com/giantswarm/agentgateway-upstream/pull/82)).

## Reason

A Session's Bearer caller credential in kagent compiles to an `actor_jwt` rule ([giantswarm/kagent-upstream#297](https://github.com/giantswarm/kagent-upstream/issues/297)). Without the mint, the Envoy gateway answered `501` and the agentgateway dataplane `403 invalid credential URI: `, so no agent could call a service as the person it works for.

## History

merged on agent-substrate/substrate `main`: [agent-substrate/substrate#2135](https://github.com/agent-substrate/substrate/pull/2135) (`66f8a888`), [#2141](https://github.com/agent-substrate/substrate/pull/2141) (`d966c8e8`), [#2421](https://github.com/agent-substrate/substrate/pull/2421) (`55b6e00e`); all three in agent-substrate/substrate v0.5.0 (2026-10-14) and kagent-dev/substrate v0.5.0-alpha4 (2026-10-14), not in the v0.5.0-alpha2 pin. Leaves at the first re-pin onto a tag that contains them.

[#421](https://github.com/giantswarm/substrate/pull/421): the `git cherry-pick -x` of the three upstream commits, with the agentgateway line's `2.4.0-rc.2` as the agentgateway dataplane; [#429](https://github.com/giantswarm/substrate/pull/429) moves the dataplane to the stable `2.4.0`

[#495](https://github.com/giantswarm/substrate/pull/495): the RPC and its messages renamed `MintActorJwt`, `MintActorJwtRequest` and `MintActorJwtResponse`, as agent-substrate/substrate#2520 (`68895abb`, 2026-10-13) named them on `main` (`pkg/proto/ateapipb/ateapi.proto` line 89 there). gRPC method names are case-sensitive: until then the line served `/ateapi.Control/MintActorJWT`, which upstream's callers, agentgateway#3985 among them, are answered `Unimplemented` on. The agentgateway line's egress (giantswarm/agentgateway-upstream:the-substrate-egress-injects-a-minted-actor-jwt) calls `MintActorJWT` up to `2.6.1` and moves in the same release window; the chart's `images.agentgateway` moves to its release in the Substrate release that carries the rename.
