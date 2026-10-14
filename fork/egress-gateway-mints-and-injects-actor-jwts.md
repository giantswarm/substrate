# The egress gateway mints and injects actor JWTs

- **Status:** carried
- **Upstream:** merged on agent-substrate/substrate `main`: [agent-substrate/substrate#2135](https://github.com/agent-substrate/substrate/pull/2135) (`66f8a888`), [#2141](https://github.com/agent-substrate/substrate/pull/2141) (`d966c8e8`), [#2421](https://github.com/agent-substrate/substrate/pull/2421) (`55b6e00e`); in no release tag yet. Leaves at the first re-pin onto a tag that contains all three.
- **Our commit:** [#421](https://github.com/giantswarm/substrate/pull/421): the `git cherry-pick -x` of the three upstream commits, with the agentgateway line's `2.4.0-rc.1` as the agentgateway dataplane

## Change

The Envoy ext_proc gateway (`cmd/atenet/internal/router/egress`) injects an `actor_jwt` entry of `replace_headers` whose header the request carries. It writes the entry's prefix plus a JWT minted through `Control.MintActorJWT` for the actor behind the tunnel, with the entry's audiences and lifetime. The `actorjwt` package caches a token per actor, sorted audiences and lifetime until a third of its lifetime remains, and concurrent misses share one mint. A failed mint denies the request: 403 when the actor is gone, 503 when ate-api is unavailable or the mint timed out, 500 otherwise. Actor JWTs need no credential provider. `MintActorJWTRequest.audience` is `audiences` (same field number). In the replay of #2141, `applyEffects` keeps the line's `ReplaceHeadersFor(effects, path)`, and upstream's `providerCredential` keeps the line's provider-named denials (`mapCredentialProviderError(provider, …)`, `credentialProviderFailed`). The agentgateway dataplane, which the line runs, makes the same injection from the agentgateway line's `2.4.0-rc.1` on ([giantswarm/agentgateway-upstream#80](https://github.com/giantswarm/agentgateway-upstream/pull/80)).

## Reason

A Session's Bearer caller credential in kagent compiles to an `actor_jwt` rule ([giantswarm/kagent-upstream#297](https://github.com/giantswarm/kagent-upstream/issues/297)). Without the mint, the Envoy gateway answered `501` and the agentgateway dataplane `403 invalid credential URI: `, so no agent could call a service as the person it works for.
