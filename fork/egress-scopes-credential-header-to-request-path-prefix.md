# The egress gateway scopes a credential header to a request path prefix

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2515](https://github.com/agent-substrate/substrate/pull/2515), open, from the branch `upstream/egress-credential-path-prefix` on upstream `main` @ `c4129c94`
- **Our commit:** [#349](https://github.com/giantswarm/substrate/pull/349): the fork's cherry-pick of the upstream commit, resolved against the line's `applyEffects`, which mints no actor JWT

## Change

`CredentialHeader` gains an optional `path_prefix` (field 5); `replace_headers` takes a header name once per prefix (list map key `header` plus `path_prefix`). For each header the gateway applies the entry with the longest prefix the request path is under, the entry without a prefix otherwise (`egresspolicy.ReplaceHeadersFor`, called from `applyEffects` in `cmd/atenet/internal/router/egress/credentials.go`, shared by the Envoy and agentgateway dataplanes). A prefix matches the path without its query at a segment boundary; a path with a dot or empty segment, a backslash, or a percent-encoded `.`, `/` or `\` matches no prefix and gets only the host-wide entry. Admission refuses two entries for one header (case-insensitive) and prefix, and a prefix in any of those forms or over 1024 bytes. A policy without prefixes reads back and applies as before; `tls_passthrough` rules carry no effects. Unit tests in `internal/egresspolicy`, the gateway and `apivalidation`; the `egresscredinject` e2e binds `Basic` host-wide and `Bearer` under one prefix on one host and proves the refused duplicate leaves the stored policy unchanged.

## Reason

One host can take different credentials on different paths: a Git server takes `Basic` for Git over HTTPS and `Bearer` for its REST API under `/api/`. With one credential per header and host, an agent could either push with git or call the API, never both. Path-scoped caller credentials in kagent and a workspace's Git host bindings build on it.
