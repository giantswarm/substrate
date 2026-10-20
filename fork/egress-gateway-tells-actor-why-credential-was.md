---
id: egress-gateway-tells-actor-why-credential-was
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(atenet): tell the actor why a credential was refused'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2555, agent-substrate/substrate#2706, agentgateway/agentgateway#3875, agentgateway/agentgateway#2162]
  exit: "agent-substrate/substrate#2706 merged: re-pin; the reason-to-the-actor part drops at the agentgateway re-pin past be8864a88 (agentgateway/agentgateway#2162, merged 2026-10-21); re-check each re-pin"
depends_on: [giantswarm/agentgateway-upstream:a-credential-providers-failure-is-answered-by-its-class, agentgateway/agentgateway#2162]
verified: 2026-10-22
---
# The egress gateway tells the actor why a credential was refused

## Change

The egress gateway tells the actor why a credential was refused (`mapCredentialProviderError` in `cmd/atenet/internal/router/egress/credentials.go`, the contract on `FetchSecret` in `pkg/proto/credproviderpb/credprovider.proto`: `NotFound`, `PermissionDenied`, `FailedPrecondition` and `Unauthenticated` deny as 403 with the provider's message, escaped and cut to 512 bytes; `Unavailable`, `DeadlineExceeded` and `ResourceExhausted` as 503; anything else, and an empty or unusable secret, as 502; every body names the provider and nothing of the transport)

## Reason

kagent's caller credential provider refuses for reasons the actor can act on (no turn running, no grant at the token broker: sign in and retry), and every refusal reached the actor as `egress denied`; git prints a `text/plain` error body as `remote:` lines, so the reason is what a coding agent relays to its person. A provider bug answered 403 like a refusal, which an agent reads as a permission problem: 502 is the gateway's own outcome when the provider it relies on misbehaves. This departs from upstream's deliberate 403 for anything unexpected (chosen so a client does not retry); no answer becomes 404 or 400, which git and `gh` read as a missing repository or a malformed request. The agentgateway dataplane applies the same table (giantswarm/agentgateway-upstream `fork/credential-provider-errors`)

## History

[agent-substrate/substrate#2555](https://github.com/agent-substrate/substrate/issues/2555) (open, 2026-10-13) asks upstream for the classes; ericcurtin took the Substrate side on 2026-10-15 (`credentials.go`, tests, doc table, the `FetchSecret` contract; the config-error row waits until 500 vs 502 is settled); the agentgateway half is agentgateway/agentgateway#3875 (draft)

`feat/credential-provider-errors`; merged as [#112](https://github.com/giantswarm/substrate/pull/112) (`feat(atenet): tell the actor why a credential was refused`)

2026-10-20: the Substrate side is [agent-substrate/substrate#2706](https://github.com/agent-substrate/substrate/pull/2706) (ericcurtin, opened 2026-10-15, "Fixes #2555"): provider `Internal`, `Unknown`, `Unimplemented` and an unusable credential answer 502, `ResourceExhausted` 503, `InvalidArgument` stays 403; the outcome stays open on it.

2026-10-20, decided on internal issue 38652, comment 6301541135: class 5 upstream PR in flight; destination agent-substrate/substrate#2706 (a third-party PR for #2555) then agentgateway/agentgateway#3875; first step reopen agent-substrate/substrate#2555, which was closed while #2706 is open; branch the `giantswarm` commit of this file; test the fork e2e error-class cases; depends on agentgateway/agentgateway#2162 (if it lands no reason reaches the actor); owner unassigned; draft: internal issue 38652, comment 6301545434.

2026-10-22, updated on internal issue 38652: agentgateway/agentgateway#2162 merged on 2026-10-21, so every egress refusal body is generic upstream and no reason reaches the actor; agent-substrate/substrate#2555 was reopened on 2026-10-21 (it had been closed by a ledger pull request's closing keyword, not by a maintainer).
