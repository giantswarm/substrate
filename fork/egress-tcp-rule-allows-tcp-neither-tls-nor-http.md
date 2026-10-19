---
id: egress-tcp-rule-allows-tcp-neither-tls-nor-http
fork: substrate
theme: egress-policies
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file an upstream issue first (egress API locked for GA), then the branch upstream/egress-tcp-rule
depends_on: [ate-api-reports-egress-policy-contract-kubectl, giantswarm/agentgateway-upstream:the-substrate-egress-relays-tcp-a-tcp-rule-allows]
commit: giantswarm/substrate#479
verified: 2026-10-19
---
# An egress tcp rule allows TCP that is neither TLS nor HTTP

- **Status:** carried
- **Upstream:** prepared in the fork, branch `upstream/egress-tcp-rule` on upstream `main`; not filed yet. Upstream's maintainers call the egress API locked for GA and ask for API changes as issues first.
- **Our commit:** [#479](https://github.com/giantswarm/substrate/pull/479): the rule, its validation, the agentgateway TCP listener, tests and docs, and a fork-only commit that moves the line's egress policy contract to `v0.4.0-alpha1+path-prefix+tcp`

## Change

`EgressRule` gains a `tcp` member (`TCPRule`, field 4 of the `rule` oneof): exact hostnames and CIDRs with a required port selection. The gateway decides a connection by its first bytes: a TLS ClientHello by the `tls_passthrough` and `https` rules, an HTTP request by the `http` rules, anything else by the `tcp` rules, on the address and port the actor connected to. A hostname matches when the gateway resolves it to that address; the actor's own resolution plays no part. The addresses of the gateway and its node (loopback, link-local, the clouds' metadata and platform services, the gateway's own address, and their NAT64 forms) are reached only through a CIDR inside their range, never through a hostname or a wider CIDR. Admission requires at least one hostname or CIDR and refuses a CIDR with host bits past its prefix. The agentgateway egress config gains the TCP listener the rule's streams are relayed through; the relay itself is agentgateway's (the agentgateway line's release this line pins, giantswarm/agentgateway-upstream#101). The Envoy dataplane has no `tcp` chain and keeps closing such connections. `kubectl ate` reads and writes the rule. Unit tests in `internal/egresspolicy`, `apivalidation` and `kubectl-ate`; the networking e2e (`TestActorEgressTCPByAddress`) relays a client-first exchange to a listed origin and sees a connection to an unlisted one closed with no bytes; the egress demo gains `POST /tcp`, the e2e test server a `tcp-echo` mode.

## Reason

An agent could not use git over SSH, or a database or message broker whose protocol is neither TLS nor HTTP, even when its author declared the host: the gateway closed every such connection.

## Re-pin

`tcp` is field 4 of `EgressRule`'s oneof, a number upstream has not assigned. While upstream does not carry the rule, every re-pin checks the new pin's `EgressRule` in `pkg/proto/ateapipb/ateapi.proto`. If upstream gave field 4 (or the name `tcp`) another meaning, the replay stops, and the line moves its field to a free number with an automatic migration of stored policies before the re-pin lands. If upstream adopts the rule as proposed, the re-pin drops this commit and the contract suffix `+tcp` with it.
