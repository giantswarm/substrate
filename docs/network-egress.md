# Network Egress Contract

Last updated: 09/18/2026

## Overview

Agent Substrate enforces that all outbound traffic from an actor, except traffic sent to TCP or UDP destination port 53, MUST go through an egress policy enforcement point (PEP). That PEP can (optionally) inspect, filter, or redirect the traffic according to substrate-native egress policies. This document describes the contract between substrate and egress PEPs.

## Transport

At present, actor TCP traffic exits the sandbox through [`atunnel`](./glossary.md#components) and is forwarded on to the PEP, except traffic to destination port 53; DNS over TCP or UDP port 53 goes through the actor DNS relay instead of the PEP. The relay forwards a query only when every question names a hostname allowed by the actor’s egress policy; denied names receive NXDOMAIN without reaching an upstream resolver, while all other protocols are blocked. `atunnel` is configured with the PEP's network address and port at startup and uses that address to open an mTLS HTTP/1.1 CONNECT tunnel to the PEP. The CONNECT request target and `Host` header are both set to the original destination IP and port that `atunnel` derived from the actor's TCP connection. Actor networking is currently IPv4-only.

## Trust Boundary

In substrate, we consider everything originating from the actor to be untrusted. The egress PEP MUST NOT trust any decision or attestation coming from the actor itself; it MUST instead trust only information carried through substrate-controlled channels (e.g. the actor certificate and the CONNECT request produced by atunnel). For example, the PEP MUST NOT treat an actor-provided HTTP hostname or TLS SNI as proof that the original destination has that name. The PEP MAY use the hostname as a policy input, but when authorization is based on that hostname, it MUST route the traffic to the authorized hostname rather than use the hostname to authorize an arbitrary IP address from the CONNECT request.

## Server-side TLS

The egress PEP MUST serve TLS using a certificate that is valid for the configured PEP hostname and chains to a CA trusted by `atunnel`. `atunnel` uses the configured PEP hostname as the TLS server name and MUST reject the connection if it cannot verify the PEP's certificate.

In the default Kubernetes deployment, the PEP obtains its certificate and private key through a projected `podCertificate` volume using substrate's Service DNS signer (`servicedns.podcert.ate.dev/identity`). The signer derives the certificate's DNS SANs from the Services that select the PEP Pod, so the PEP MUST be selected by a Service and the hostname configured in `atunnel` MUST match one of those Service DNS names. For the default PEP, that name is `atenet-egress.ate-system.svc`. The projected `credential-bundle.pem` contains the serving certificate chain and private key, while the `ClusterTrustBundle` for the same signer supplies `atunnel` with the corresponding trust anchors.

## Client Authorization

`atunnel` MUST present an actor-specific client certificate when connecting to the PEP. At actor activation, `atunnel` generates a private key and requests a short-lived certificate from substrate through the node-local atelet. The private key remains in `atunnel`. The certificate identifies the actor by its atespace, name, and UID and is scoped to the `atunnel` purpose.

The PEP MUST require and verify the client certificate before accepting a CONNECT request. It MUST verify the certificate chain and lifetime against the Actor Identity CA, require the client authentication usage, and require exactly one valid `ActorIdentity` extension whose atespace, actor name, actor UID, and purpose are present. The current extension OID is `1.3.6.1.4.1.11129.2.12.2`; it is allocated under Google's enterprise number and will change after the CNCF donation is complete. The purpose MUST be `atunnel`, and the certificate's actor URI SAN MUST identify the same actor as the extension. The PEP MUST then confirm with ate-api-server that the actor still exists, that its current UID matches the certificate, and that it is running. The PEP MUST reject the CONNECT request if any of these checks fail.

```text
CURRENT CONNECT EGRESS PATH (one tunnel per actor TCP connection except destination port 53)

  Actor sandbox                 Worker pod / ateom                  Egress PEP                     Authorized upstream
  +------------------+          +--------------------------+        +-------------------------+    +------------------+
  | Actor process    |          | nftables                 |        | mTLS listener           |    | Target selected  |
  | (untrusted)      |          | TCP REDIRECT             |        | + CONNECT terminator    |    | under policy     |
  +--------+---------+          +------------+-------------+        +------------+------------+    +--------+---------+
           | connect(IP:port)                |                                   |                          |
           +-------------------------------->|                                   |                          |
                                             | SO_ORIGINAL_DST                   |                          |
                                             v                                   |                          |
                                +------------+-------------+                     |                          |
                                | atunnel                  |                     |                          |
                                | (trusted tunnel client)  |                     |                          |
                                +------------+-------------+                     |                          |
                                             |                                   |                          |
                                             | TLS 1.2+ mutual authentication    |                          |
                                             | actor-specific client certificate |                          |
                                             +---------------------------------->| verify certificate and   |
                                             |                                   | authorize current actor  |
                                             |                                   |                          |
                                             | HTTP/1.1 CONNECT IP:port          |                          |
                                             | Host: IP:port                     |                          |
                                             +---------------------------------->| validate destination;    |
                                             |                                   | evaluate egress policy   |
                                             |                                   |                          |
                                             | HTTP/1.1 2xx                      |                          |
                                             |<----------------------------------+                          |
                                             |                                   | CIDR/all policy:         |
                                             |                                   | dial now;                |
                                             |                                   | hostname policy:         |
                                             |                                   | inspect inner request    |
                                             |                                   | before dialing           |
                                             |                                   +------------------------->|
                                             |                                   |                          |
           |<================ raw bidirectional bytes inside CONNECT ===========>|<========================>|
           |             (PEP may pass through, inspect, redirect, or MITM according to policy)             |
```

## Actor DNS policy

The relay checks hostnames from HTTP, HTTPS, TLS passthrough and TCP rules using the same exact-name and wildcard matching as the egress gateway. It checks every message of a TCP DNS stream separately. A nil or empty policy permits no DNS names. A policy read failure before the first successful fetch denies resolution; after a successful fetch, the last policy remains in use until the control plane is reachable again. Policies are refreshed after five seconds.

The worker reads only destinations through atelet’s authenticated node-local connection. Policy metadata and credential injection effects remain outside the worker. DNS names are normalized to lowercase without a trailing dot before matching. An exact rule does not admit arbitrary subdomains; a wildcard rule admits the subdomains the policy explicitly permits. DNS record types are unrestricted for allowed names.

Short service names also match their expansions under the worker’s Kubernetes cluster search domains. Node-supplied external search domains do not expand the allowlist.
