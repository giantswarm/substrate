# The egress gateway tells the actor why a credential was refused

- **Status:** carried
- **Upstream:** to file, with the agentgateway half: the 502 for unexpected answers needs upstream's agreement
- **Our commit:** `feat/credential-provider-errors`; merged as [#112](https://github.com/giantswarm/substrate/pull/112) (`feat(atenet): tell the actor why a credential was refused`)

## Change

The egress gateway tells the actor why a credential was refused (`mapCredentialProviderError` in `cmd/atenet/internal/router/egress/credentials.go`, the contract on `FetchSecret` in `pkg/proto/credproviderpb/credprovider.proto`: `NotFound`, `PermissionDenied`, `FailedPrecondition` and `Unauthenticated` deny as 403 with the provider's message, escaped and cut to 512 bytes; `Unavailable`, `DeadlineExceeded` and `ResourceExhausted` as 503; anything else, and an empty or unusable secret, as 502; every body names the provider and nothing of the transport)

## Reason

kagent's caller credential provider refuses for reasons the actor can act on (no turn running, no grant at the token broker: sign in and retry), and every refusal reached the actor as `egress denied`; git prints a `text/plain` error body as `remote:` lines, so the reason is what a coding agent relays to its person. A provider bug answered 403 like a refusal, which an agent reads as a permission problem: 502 is the gateway's own outcome when the provider it relies on misbehaves. This departs from upstream's deliberate 403 for anything unexpected (chosen so a client does not retry); no answer becomes 404 or 400, which git and `gh` read as a missing repository or a malformed request. The agentgateway dataplane applies the same table (giantswarm/agentgateway-upstream `fork/credential-provider-errors`)
