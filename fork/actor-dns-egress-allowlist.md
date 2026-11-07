---
id: actor-dns-egress-allowlist
fork: substrate
theme: egress-policies
category: bugfix
status: carried
commits:
  - 'fix(dns): resolve only hostnames allowed by actor egress policy'
upstream:
  outcome: merged
  refs: [agent-substrate/substrate#2524]
  exit: re-pin onto the upstream actor DNS policy evaluator and policy-source RPC
depends_on: [actor-dns-queries-counted-by-template, egress-tcp-rule-allows-tcp-neither-tls-nor-http]
verified: 2026-11-07
---
# Actor DNS follows the egress allowlist

## Change

The sandbox DNS relay checks every question against the actor's HTTP, HTTPS,
TLS passthrough and TCP hostname rules before contacting a resolver. A denied
name receives NXDOMAIN. UDP and every message of a TCP stream are checked;
malformed queries are never forwarded. Both sandbox runtimes obtain the
policy from atelet over the authenticated node-local connection. The worker
receives only destinations, with no policy metadata or credential effects.
The upstream five-second policy cache and fail-static refresh behavior are
retained. The relay keeps its connection limits, cancellation, query counters
and UDP resolver failover.

## Reason

Placement: upstream already has this mechanism, in the actor DNS evaluator
and the GetActorEgressPolicy worker RPC. The current pin predates it. Carry
those parts on the existing relay rather than re-pin the runtime's unrelated
API and storage changes. Include the line's TCP hostname rules so an allowed
TCP destination can still be resolved. No persisted format changes.

## History

Adapted from upstream commit df1331d75cd5f109421cd06e830f22841cf38395,
which checks individual hostnames as well as refusing actors without a policy.
The relay implementation at this pin differs from upstream's DNS handler,
so policy checks are applied before UDP forwarding and per framed TCP message.

[Pull request](https://github.com/giantswarm/substrate/pull/560).
