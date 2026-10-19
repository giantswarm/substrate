---
id: egress-path-prefix-admits-encoded-slash
fork: substrate
theme: credential-injection
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: goes with the path_prefix change (agent-substrate/substrate#2541)
depends_on: [egress-scopes-credential-header-to-request-path-prefix, giantswarm/agentgateway-upstream:substrate-egress-path-prefix-admits-an-encoded-slash]
commit: giantswarm/substrate#483
verified: 2026-10-19
---
# A path-prefixed credential header applies to a path with an encoded slash

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch `upstream/egress-path-prefix-encoded-slash` on top of `upstream/egress-credential-path-prefix`, so it goes upstream with or after the path-prefix change. Drops at the re-pin onto a commit containing it
- **Our commit:** [#483](https://github.com/giantswarm/substrate/pull/483), for [#481](https://github.com/giantswarm/substrate/issues/481)

## Change

`isCanonicalPath` in `internal/egresspolicy/pathprefix.go` accepts a percent-encoded `/` when the path has no dot or empty segment read either way, `%2F` taken as a separator as well; a percent-encoded `.` or `\` still makes a path non-canonical. A prefix still compares byte for byte, so an encoded slash never moves a path under a prefix it is not under as written, and `ValidatePathPrefix` keeps refusing a `%2F` in a prefix. The `path_prefix` comment in `ateapi.proto` and `docs/egress-credential-injection.md` state the rule. `TestReplaceHeadersForEncodedSlash` covers GitLab's encoded project, file and branch paths; `TestReplaceHeadersForEscapes` gains the encoded-slash escape forms. The same rule and cases as the agentgateway egress's `canonical_path` ([giantswarm/agentgateway-upstream#106](https://github.com/giantswarm/agentgateway-upstream/pull/106)).

## Reason

GitLab addresses a project by its URL-encoded path (`/api/v4/projects/<group>%2F<project>`), and a credential for its API is scoped to `/api/v4/`. With every `%2F` path non-canonical, such a request reached GitLab without the credential and was answered `404 Project Not Found`, so `glab api projects/<group>%2F<project>` and `glab mr create` failed from a Session.
