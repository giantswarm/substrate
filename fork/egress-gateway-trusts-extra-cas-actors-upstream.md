---
id: egress-gateway-trusts-extra-cas-actors-upstream
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(chart): extra CAs for the egress gateway''s upstream TLS'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#100, kagent-dev/substrate#101]
  exit: "kagent-dev/substrate#101 merged: drop at the re-pin past it; unreviewed on 2026-11-17: the lab mounts its own bundle and the file drops (no production installation needs it, checked 2026-10-20)"
depends_on: []
verified: 2026-10-26
---
# The egress gateway trusts extra CAs on the actors' upstream HTTPS

## Change

The egress gateway trusts extra CAs on the actors' upstream HTTPS (`atenetEgress.upstreamTrust.caBundle`, a PEM bundle; the chart renders it as the ConfigMap `atenet-egress-upstream-trust`, mounts it on the egress agentgateway and sets `SSL_CERT_DIR` next to the image's public bundle in `SSL_CERT_FILE`, which rustls-native-certs then reads both of; a checksum annotation rolls the gateway on a change; empty by default, the rendered manifests unchanged)

## Reason

the egress gateway terminates the actors' TLS and dials the upstream itself with its image's public roots, so a model or tool endpoint behind a private CA (the lab's models Gateway, an in-cluster Gateway with an installation CA, a corporate proxy) fails every call with `invalid peer certificate: UnknownIssuer` and a `503` to the actor, whatever CA the runtime itself trusts; the route's `backendTLS.root` would replace the public roots instead of adding to them (acceptance test `agentlab serving-test`, [agentlab#284](https://github.com/giantswarm/agentlab/issues/284))

## History

to file — prepared in the fork: branch [`upstream/atenet-egress-upstream-trust`](https://github.com/giantswarm/substrate/tree/upstream/atenet-egress-upstream-trust) here (`175664c9`, on the mirror `main` @ `bf51a996`; its message is the pull-request text). A team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it

[#92](https://github.com/giantswarm/substrate/pull/92)

2026-10-20, decided on internal issue 38652, comment 6301541135: class 5 upstream PR, capped (136 lines, acceptance uncertain), fallback class 3 (lab only); destination kagent-dev/substrate, an issue then the chart PR; first step the issue, kagent-dev/substrate; branch `upstream/atenet-egress-upstream-trust` @ `175664c9f`; test `atenet_egress_upstream_trust_test.yaml` and the lab's serving test; depends on no theme; re-check 2026-11-17; owner unassigned; draft: internal issue 38652, comment 6301544635 and comment 6301545100.

2026-10-26: filed by fiunchinho as the issue kagent-dev/substrate#100 and the chart PR kagent-dev/substrate#101, from `upstream/atenet-egress-upstream-trust-2` @ `7a20f7e15` (`175664c9f` cherry-picked onto kagent-dev/substrate `main` @ `3c6b9adbf`, +167). One change on top of the prepared commit: with a bundle set, the dynamic HTTPS backend leaves `backendTLS.root` out, because agentgateway reads the native roots only when `root` is unset and upstream `main` sets it on that backend; without a bundle the render is identical to `main`. `helm unittest` 44/44, two new cases for `root`. Owner fiunchinho.
