---
id: atenet-router-answers-denial-callers-protocol-grpc
fork: substrate
theme: credential-injection
category: bugfix
status: carried
commits:
  - 'fix(atenet): answer a denied gRPC call with the denial''s gRPC code'
upstream:
  outcome: to-file
  refs: []
  exit: "filed low priority from the prepared branch when a slot frees; the commit leaves the giantswarm branch at the next re-pin whatever the PR's state (the Envoy dataplane is not on this line's path)"
depends_on: []
verified: 2026-10-20
---
# The atenet router answers a denial in the caller's protocol

## Change

The atenet router answers a denial in the caller's protocol: a gRPC call (`content-type` `application/grpc`, alone or with a `+format` suffix or parameters) gets a trailers-only gRPC response — HTTP 200, no body, `content-type: application/grpc`, `grpc-status`, the percent-encoded `grpc-message`, and the code in the immediate response's `grpc_status` — whose code is the gRPC status of the denial's cause (Aborted for a held actor, NotFound for a missing one, ResourceExhausted for a full pool), else the code nearest the HTTP status, and whose message is the denial's client-safe message; every other request (gRPC-Web included) keeps its status code, `text/plain` and body (`cmd/atenet/internal/router/extproc`; unit tests on the mux, e2e `TestIngressDenialProtocol` in the networking suite)

## Reason

a gRPC caller of a held actor (one whose lease another operation holds, answered Aborted by ate-api) got `Unavailable`: Envoy turned the router's plain-text 503 into a gRPC status by gRPC's HTTP-to-status table, so the caller could not tell a held actor, whose operation it should retry, from an unavailable control plane (a missing actor arrived as `Unimplemented`) ([giantswarm/substrate#158](https://github.com/giantswarm/substrate/issues/158); proven with the dataplane's Envoy v1.39 in the pull request). The AgentGateway dataplane, which this line's chart runs, answers its own ingress denials by the same fallback table (a missing actor `Unimplemented`, a held one `Unknown`), which the e2e records per dataplane; its counterpart is [giantswarm/agentgateway-upstream#43](https://github.com/giantswarm/agentgateway-upstream/issues/43)

## History

to file: the upstream-shaped patch is branch [`upstream/atenet-grpc-denial-code`](https://github.com/giantswarm/substrate/tree/upstream/atenet-grpc-denial-code) here (`0a91139b`, on the mirror `main`); agent-substrate/substrate `main` (`296e329a`, 2026-10-08) still answers every denial as `text/plain` and no upstream issue or pull request covers it (searched 2026-10-08); a team member opens the agent-substrate/substrate pull request with DCO sign-off once the team has reviewed it

`giantswarm`: [#200](https://github.com/giantswarm/substrate/pull/200)

2026-10-20, decided on internal issue 38652, comment 6301541135: class 5 upstream PR, low priority, uncarried at the next re-pin; destination agent-substrate/substrate; first step the PR when a slot frees; branch `upstream/atenet-grpc-denial-code` @ `0a91139b8`; test `TestIngressDenialProtocol`; depends on no theme; owner unassigned; draft: internal issue 38652, comment 6301546603.
