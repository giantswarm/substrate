---
id: podcertificates-maxexpirationseconds-caps-every-pod-certific
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'chart: podCertificates.maxExpirationSeconds caps every pod certificate'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1344]
  exit: kd chart PR
depends_on: []
verified: 2026-10-19
---
# `podCertificates.maxExpirationSeconds` caps every pod certificate the chart requests

## Change

`podCertificates.maxExpirationSeconds` caps every pod certificate the chart requests (`maxExpirationSeconds` on all eleven `podCertificate` sources through the named template `substrate.podCertificateMaxExpiration`; unset by default, so the committed manifests do not change)

## Reason

the signers issue 24h and kubelet rewrites each bundle 30 min before expiry; nothing in the chart could shorten that, so a component that keeps a certificate past the rotation was found in production, not in a test: the egress gateway's agentgateway presented its start-time pod identity certificate to ate-api after 24h and every actor CONNECT was denied with `CertificateExpired`. With `3600` a lab installation rotates every pod certificate within the hour; the rotation proof of the agentgateway line's fix (giantswarm/agentgateway-upstream#20) runs on it

## History

to file: the upstream-shaped patch is branch [`upstream/podcert-max-expiration`](https://github.com/giantswarm/substrate/tree/upstream/podcert-max-expiration) here (`9f12a42a`, on the mirror `main` @ `8316538f`); a team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it. No upstream item covers it (agent-substrate/substrate and kagent-dev/substrate pull requests searched 2026-09-23: agent-substrate#695 reloads the atunnel client CA per handshake, #1344 caches the CSI CA pool; neither exposes the pod certificate lifetime)

`bf2728fe` ([#58](https://github.com/giantswarm/substrate/pull/58))
