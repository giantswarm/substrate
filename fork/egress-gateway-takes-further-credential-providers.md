# The egress gateway takes further credential providers

- **Status:** carried
- **Upstream:** to file for kagent-dev/substrate (chart only)
- **Our commit:** [#248](https://github.com/giantswarm/substrate/pull/248) (carries [#104](https://github.com/giantswarm/substrate/pull/104) onto the v0.4.0-alpha1 line)

## Change

The egress gateway takes further credential providers (`credentialProvider.additionalProviders`: `uriAuthority`, a lowercase DNS name, and `host`, `<service>.<namespace>.svc:<port>`, rendered on both egress listeners beside the bundled `k8s.io` provider, dialed with the gateway's pod identity and verified against the `servicedns.podcert.ate.dev` trust bundle; an entry cannot replace `k8s.io` or repeat an authority; `charts/substrate/tests/atenet_egress_credential_providers_test.yaml`)

## Reason

kagent injects the caller of the current turn at egress through its own provider, so the caller's bearer never enters the actor: agentgateway already selects a provider by URI authority and fetches the actor's egress policy on every request, but the chart rendered one provider only
