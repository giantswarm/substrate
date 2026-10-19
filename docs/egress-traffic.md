This document specifies the supported egress traffic for [GA](https://github.com/agent-substrate/substrate/milestone/3).

Actor TCP egress (except DNS to the sandbox's gateway) is redirected to atunnel,
which opens a CONNECT tunnel to the egress gateway. Egress gateway applies policy.

DNS over UDP and TCP goes to the sandbox's DNS relay, which forwards a query
to the worker pod's resolvers only when the queried hostname matches a host or
host wildcard in the actor's egress policy. Queries from an actor without a
policy, or for a hostname not allowed by any rule, are answered `NXDOMAIN`.
A policy created, updated, or deleted while the actor runs applies to its DNS
within about 5 seconds.

Other UDP and other traffic is filtered by nftables and never reaches the gateway.

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
| any | Standard HTTP(S) CONNECT (forward-proxy tunnel) | Blocked | n/a | `403 Forbidden` (Envoy), `405 Method Not Allowed` (agentgateway) |
| 53 | DNS to the sandbox's gateway | Allowed for hostnames matching the actor's egress policy | sandbox DNS relay -> worker pod's resolvers | `NXDOMAIN` |
| any | Other TCP the client speaks first on (SSH, PostgreSQL, Redis, ...) | Supported with policy controls: `tcp` rules, on the address and port the actor connected to (agentgateway dataplane) | atunnel -> egress gateway -> that address and port, relayed without being read | The connection is accepted and then closed with no bytes returned. There is no status code. atunnel logs the failure. |
| any | TCP the server speaks first on (MySQL, SMTP, ...) | Blocked | n/a | The connection is accepted and closed, with no bytes returned, once the gateway's protocol detection times out. |

## UDP


| Port | Traffic | Behavior | Path | What the actor sees when refused |
| :---- | :---- | :---- | :---- | :---- |
| 53 | DNS to the sandbox's gateway | Allowed for hostnames matching the actor's egress policy | sandbox DNS relay -> worker pod's resolvers | `NXDOMAIN` |
| any other | Any other UDP | Blocked | n/a | Packets are dropped, not rejected: no ICMP port-unreachable is sent, so the client hangs until its own timeout. |

## Other protocols

Everything that is neither TCP nor UDP is blocked. Packets are dropped, not rejected: no ICMP port-unreachable is sent, so the client hangs until its own timeout.

## Requesting support

If you would like Substrate to support egress traffic that is blocked above, please
[file an issue](https://github.com/agent-substrate/substrate/issues/new)
describing your use case.
