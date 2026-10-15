# atunnel ends a CONNECT tunnel when the actor closes its connection

- **Status:** carried
- **Upstream:** the relay change of upstream's own commit `da35a983` ([agent-substrate/substrate#2531](https://github.com/agent-substrate/substrate/pull/2531), merged), in every upstream release since v0.5.0-alpha4; a backport of that change alone, nothing to queue upstream, which leaves the line at the first re-pin onto v0.5.0-alpha4 or later
- **Our commit:** [#451](https://github.com/giantswarm/substrate/pull/451)

## Change

`internal/atunnel`'s `relayIngressWithHalfClose` closes the router's side of a CONNECT tunnel as soon as the actor
stops sending, and still half-closes the actor's connection when the router stops sending. #2531's Envoy CONNECT
router and its log plumbing are not part of the backport. Unit test `TestRelayIngressEndsWhenActorCloses`: an actor
that answers and closes, and one that resets the connection, while the router's request stream stays open.

## Reason

The tunnel ended only once both directions had ended, so it outlived an actor's connection for as long as the
router held its request stream open. A gRPC call to an HTTP/1.1-only actor takes that path: the actor answers the
router's HTTP/2 preface with an HTTP/1.1 response and closes. When that answer was lost, the router waited for the
actor's SETTINGS frame until its client's timeout instead of failing the call at once. The networking e2e's
`TestIngressProtocolDowngrade` hit it as a 30 s timeout ([#450](https://github.com/giantswarm/substrate/issues/450)).
