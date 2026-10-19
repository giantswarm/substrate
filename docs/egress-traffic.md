This document specifies the supported egress traffic for [GA](https://github.com/agent-substrate/substrate/milestone/3).

Actor TCP egress (except DNS traffic on port 53) is redirected to atunnel,
which opens a CONNECT tunnel to the egress gateway. Egress gateway applies policy.

DNS-over-TCP, UDP and other traffic is filtered by nftables and never reaches the gateway.

The gateway decides a connection by its first bytes: a TLS ClientHello by the
`https` and `tls_passthrough` rules, an HTTP request by the `http` rules, and
anything else by the `tcp` rules. A `tcp` rule never decides TLS or HTTP to the
same destination. It matches a CIDR containing the address the actor
connected to, or a hostname the gateway resolves to that address (the actor's
own resolution plays no part). The addresses of the gateway and its node
(loopback, link-local, the clouds' metadata and platform services, the gateway's
own address, and their NAT64 forms) are reached only through a CIDR inside their
range, never through a hostname or a wider CIDR. The Envoy dataplane has no `tcp` rule yet
and closes every such connection.

This applies to an actor's own telemetry too: an OpenTelemetry SDK inside the actor
cannot reach the collector until the actor has an `http` egress rule for it. See
[Actor telemetry needs an egress policy](observability.md#actor-telemetry-needs-an-egress-policy).

## TCP

| Port | Traffic | Behavior | Path | What the actor sees when refused |
| :---- | :---- | :---- | :---- | :---- |
| any | HTTP(S) 1.1 / 2 | Supported with policy controls | atunnel -> egress gateway -> origin | `403 Forbidden` |
| any | WebSocket | Blocked | n/a | `403 Forbidden` |
| any | Standard HTTP(S) CONNECT (forward-proxy tunnel) | Blocked | n/a | `403 Forbidden` |
| 53 | DNS | Allowed via netfilter rules | nftables -> node-configured DNS | n/a |
| any | TLS (SNI) | Supported with policy controls: `tls_passthrough` rules | atunnel -> egress gateway -> origin, without decryption | The connection is closed during the handshake. |
| any | Other TCP the client speaks first on (SSH, PostgreSQL, Redis, ...) | Supported with policy controls: `tcp` rules, on the address and port the actor connected to (agentgateway dataplane) | atunnel -> egress gateway -> that address and port, relayed without being read | The connection is accepted and then closed with no bytes returned. There is no status code. atunnel logs the failure. |
| any | TCP the server speaks first on (MySQL, SMTP, ...) | Blocked | n/a | The connection is accepted and closed, with no bytes returned, once the gateway's protocol detection times out. |

## UDP


| Port | Traffic | Behavior | Path | What the actor sees when refused |
| :---- | :---- | :---- | :---- | :---- |
| 53 | DNS | Allowed via netfilter rules | nftables -> node-configured DNS | n/a |
| any other | Any other UDP | Blocked | n/a | Packets are dropped, not rejected: no ICMP port-unreachable is sent, so the client hangs until its own timeout. |

## Other protocols

Everything that is neither TCP nor UDP is blocked. Packets are dropped, not rejected: no ICMP port-unreachable is sent, so the client hangs until its own timeout.

## Requesting support

If you would like Substrate to support egress traffic that is blocked above, please
[file an issue](https://github.com/agent-substrate/substrate/issues/new)
describing your use case.