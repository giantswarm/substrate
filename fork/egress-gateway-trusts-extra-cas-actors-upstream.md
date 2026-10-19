---
id: egress-gateway-trusts-extra-cas-actors-upstream
fork: substrate
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: kd chart PR
depends_on: []
commit: giantswarm/substrate#92
verified: 2026-10-19
---
# The egress gateway trusts extra CAs on the actors' upstream HTTPS

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch [`upstream/atenet-egress-upstream-trust`](https://github.com/giantswarm/substrate/tree/upstream/atenet-egress-upstream-trust) here (`175664c9`, on the mirror `main` @ `bf51a996`; its message is the pull-request text). A team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it
- **Our commit:** [#92](https://github.com/giantswarm/substrate/pull/92)

## Change

The egress gateway trusts extra CAs on the actors' upstream HTTPS (`atenetEgress.upstreamTrust.caBundle`, a PEM bundle; the chart renders it as the ConfigMap `atenet-egress-upstream-trust`, mounts it on the egress agentgateway and sets `SSL_CERT_DIR` next to the image's public bundle in `SSL_CERT_FILE`, which rustls-native-certs then reads both of; a checksum annotation rolls the gateway on a change; empty by default, the rendered manifests unchanged)

## Reason

the egress gateway terminates the actors' TLS and dials the upstream itself with its image's public roots, so a model or tool endpoint behind a private CA (the lab's models Gateway, an in-cluster Gateway with an installation CA, a corporate proxy) fails every call with `invalid peer certificate: UnknownIssuer` and a `503` to the actor, whatever CA the runtime itself trusts; the route's `backendTLS.root` would replace the public roots instead of adding to them (acceptance test `agentlab serving-test`, [agentlab#284](https://github.com/giantswarm/agentlab/issues/284))
