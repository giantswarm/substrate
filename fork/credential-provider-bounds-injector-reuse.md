---
id: credential-provider-bounds-injector-reuse
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(credprovider): a provider bounds how long the injector reuses its answer'
  - "docs(credprovider): say how a provider bounds the gateway's credential cache"
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR merged or declined
depends_on: []
verified: 2026-10-24
---
# A credential provider bounds how long the injector reuses its answer

## Change

`FetchSecretResponse.max_age` in `pkg/proto/credproviderpb/credprovider.proto`: unset keeps the injector's default, zero fetches again for every request, and no injector reuses an answer past its own default. atenet's Go injector keeps no cache and already fetches every request. `docs/kubernetes-credential-provider.md` says how a provider shortens the gateway's credential cache.

## Reason

The egress gateway agentgateway runs caches a secret by actor and URI for a fixed 300 s, so a provider whose answer depends on who acts on the actor at the moment (kagent answering with the token of the person whose turn runs) had it injected after that turn, into another person's next turn on a shared instance included.

## History

`feat/credential-max-age`, [#110](https://github.com/giantswarm/substrate/pull/110) (draft). The gateway side, giantswarm/agentgateway-upstream#37, was closed unmerged on 2026-10-19, parked with the caller credential provider.
