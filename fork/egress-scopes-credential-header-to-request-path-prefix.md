---
id: egress-scopes-credential-header-to-request-path-prefix
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(egress): scope a credential header to a request path prefix'
  - 'feat(ateapi): name the credential path prefix in the egress contract'
  - 'test(egress): prove a path-scoped credential cannot escape its prefix'
upstream:
  outcome: fork-only
  refs: [agent-substrate/substrate#2515, agent-substrate/substrate#2541]
  exit: "agent-substrate/substrate#2541 answered by 2026-11-17: the scheme-keyed entry accepted, the line re-seats on that commit and field 5 goes; unanswered on 2026-11-17: drops at the following re-pin, GitLab API calls as the person unsupported"
depends_on: [ate-api-reports-egress-policy-contract-kubectl, giantswarm/agentgateway-upstream:substrate-egress-applies-credential-header-by-request-path-prefix]
verified: 2026-10-20
---
# The egress gateway scopes a credential header to a request path prefix

## Change

`CredentialHeader` gains an optional `path_prefix` (field 5); `replace_headers` takes a header name once per prefix (list map key `header` plus `path_prefix`). For each header the gateway applies the entry with the longest prefix the request path is under, the entry without a prefix otherwise (`egresspolicy.ReplaceHeadersFor`, called from `applyEffects` in `cmd/atenet/internal/router/egress/credentials.go`, the Envoy dataplane's ext_proc; agentgateway's Substrate egress makes the same choice in its own `apply_effects` from the agentgateway line's 2.3.5 on, [giantswarm/agentgateway-upstream#62](https://github.com/giantswarm/agentgateway-upstream/issues/62)). A prefix matches the path without its query at a segment boundary; a path with a dot or empty segment, a backslash, or a percent-encoded `.`, `/` or `\` matches no prefix and gets only the host-wide entry. Admission refuses two entries for one header (case-insensitive) and prefix, and a prefix in any of those forms or over 1024 bytes. The line's egress policy contract (`ateapipb.EgressPolicyContractVersion`) reads `v0.4.0-alpha1+path-prefix` in a fork-only commit of this pull request, so a kubectl-ate refuses an ate-api of the other shape before it writes. A policy without prefixes reads back and applies as before; `tls_passthrough` rules carry no effects. Unit tests in `internal/egresspolicy`, the gateway and `apivalidation`, the escape cases of a `/backend-api/codex/` prefix among them (dot segments, `%2e`, `%2f` and `%5c` in either case, doubled slashes, `..;`, backslashes; with and without a host-wide entry, and never fetching the scoped credential); the `egresscredinject` e2e binds `Basic` host-wide and `Bearer` under one prefix on one host and proves the refused duplicate leaves the stored policy unchanged.

## Reason

One host can take different credentials on different paths: a Git server takes `Basic` for Git over HTTPS and `Bearer` for its REST API under `/api/`. With one credential per header and host, an agent could either push with git or call the API, never both. Path-scoped caller credentials in kagent and a workspace's Git host bindings build on it.

## Re-pin

`path_prefix` is field 5 of `CredentialHeader`, a number upstream has not assigned. While upstream does not carry the field, every re-pin checks the new pin's `CredentialHeader` in `pkg/proto/ateapipb/ateapi.proto`. If upstream gave field 5 (or the name `path_prefix`) another meaning, the replay stops, and the line moves its field to a free number with an automatic migration of stored policies before the re-pin lands: a stored policy with a prefix must read back with the same meaning. If upstream adopts the field as proposed, the re-pin drops this commit and the contract suffix `+path-prefix` with it.

## History

proposed in [agent-substrate/substrate#2541](https://github.com/agent-substrate/substrate/issues/2541) (open). Upstream's maintainers call the egress API locked for GA and ask for API changes as issues first. The implementation, [agent-substrate/substrate#2515](https://github.com/agent-substrate/substrate/pull/2515) from the branch `upstream/egress-credential-path-prefix` on upstream `main` @ `c4129c94`, is a draft that waits on the issue.

[#349](https://github.com/giantswarm/substrate/pull/349) (ledger: [#370](https://github.com/giantswarm/substrate/pull/370)): the fork's cherry-pick of the upstream commit, resolved against the line's `applyEffects`; the escape-case tests: [#468](https://github.com/giantswarm/substrate/pull/468)

2026-10-20, decided on internal issue 38652, comment 6301541135: class 6 fork-only until 2026-11-17; destination agent-substrate/substrate#2541, where the smaller scheme-keyed shape (several entries per header chosen by the prefix the request carries, no new field, no path parsing) was posted on 2026-10-20; acceptance uncertain (the API was called locked on #2515, two proto renames merged after it); no standard fits, neither dataplane has a path input; first step rework #2515 if accepted; branch `upstream/egress-credential-path-prefix` @ `35009eff8`; test `TestActorEgressCredentialInjection`; depends on no theme; owner unassigned; draft: internal issue 38652, comment 6301545833.
