# The Kubernetes credential provider mints Google access tokens

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#47](https://github.com/kagent-dev/substrate/pull/47) (open); the adapted commit on the pin is branch [`upstream/google-access-token-provider`](https://github.com/giantswarm/substrate/tree/upstream/google-access-token-provider) (`4453a6f4`), offered to #47
- **Our commit:** [#250](https://github.com/giantswarm/substrate/pull/250): `git cherry-pick -x` of the upstream-ready branch's commit, which is kagent-dev/substrate's `5ac16e7c` adapted to the `k8s.io/default` URI grammar (upstream's commit predates it and names the authority `google-access-token.kubernetes.io`)

## Change

The Kubernetes credential provider mints Google access tokens (`ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>`: the entry must hold a service account key; the provider signs its JWT assertion, exchanges it at the key's https `token_uri` for a `cloud-platform` token, caches the token per key while at least fifteen minutes remain, so the gateway's five-minute cache never serves an expired one, and returns the token, never the key; the chart and the kustomize component route both authorities, bundled names of `substrate.egressCredentialProviders` that `additionalProviders` cannot replace, on the HTTP and HTTPS egress listeners to the one provider Deployment)

## Reason

Vertex AI takes OAuth 2.0 access tokens only, and minting one means signing with the key, which the egress gateway cannot do and an actor must not hold: no kagent agent with a Vertex `ModelConfig` (Gemini or Anthropic on Vertex AI) ran on Substrate ([#142](https://github.com/giantswarm/substrate/issues/142); giantswarm/kagent-upstream#178)
