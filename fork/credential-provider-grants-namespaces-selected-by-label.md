---
id: credential-provider-grants-namespaces-selected-by-label
fork: substrate
theme: tenancy
category: gs-specific
status: carried
commits:
  - 'feat(credential-provider): grant namespaces selected by label'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2876, agent-substrate/substrate#2877, agent-substrate/substrate#2959, agent-substrate/substrate#2543, giantswarm/substrate-credential-providers#13]
  exit: 'allowedNamespaceSelector at the re-pin past as#2543 (d2b975099, golden actors in the template atespace); ownNamespaceSelector at the re-pin that carries the grant mechanism upstream picks on as#2876 after as#2959 (as#2877 closes then), or, if as#2876 is declined or unanswered by 2026-11-17, at the next re-pin with an out-of-tree Kubernetes Secrets provider in substrate-credential-providers serving class k8s.io'
depends_on: [agent-substrate/substrate#2876, agent-substrate/substrate#2543]
verified: 2026-10-20
---
# The Kubernetes credential provider grants namespaces selected by label

## Change

A namespace policy of the Kubernetes credential provider can carry an `ownNamespaceSelector` (a Kubernetes label selector, `matchLabels` and `matchExpressions`) in place of `atespace` and `allowedNamespaces`: each namespace it matches may be read by the atespace of the same name and by no other. The provider watches namespaces, only when a policy selects them, and matches each request against the namespace's labels at that moment, so a namespace created, labeled, unlabeled or deleted after startup is granted or refused without a restart; the reserved `ate-golden` atespace, which hosts the golden actor of every template, is never granted a namespace this way. An `allowedNamespaceSelector` beside `atespace` grants that atespace every namespace it matches, on the same watch: the explicit opt-in by which `ate-golden` reads tenant namespaces, whose guard against a cross-tenant read is kagent's translation of the template (it resolves the template's model, MCP servers, skills and shared templates in the template's own namespace, so the golden actor's egress policy names Secrets of that namespace only); a `secretSelector` narrows the grant as it narrows a listed one; an empty selector, an `ownNamespaceSelector` combined with `atespace`, `allowedNamespaces` or `allowedNamespaceSelector`, and an `allowedNamespaceSelector` without `atespace` fail loading. The chart renders a ClusterRole with list and watch on namespaces only when a policy has either field. Tests: `TestFetchSecretOwnNamespaceSelector` (own namespace read, cross-tenant read refused before any Secret read, a static atespace and the reserved `ate-golden` atespace gain nothing, a namespace labeled, created, unlabeled and deleted after startup), `TestFetchSecretAllowedNamespaceSelector` (the opted-in atespace reads labeled namespaces and its listed one, not an unlabeled one, follows a label added and removed after startup, and grants no other atespace anything), `TestWatchNamespacesWithoutSelectorReadsNothing`, `TestLoadNamespaceAuthorizerNamespaceSelectors`, `TestFetchSecretNamespaceSelectorsNarrowBySecretLabel`, `TestFetchSecretTenantAndGoldenSelectors`, the `namespace selector` and `allowed namespace selector` cases of `TestProviderManifests`.

## Reason

The Agent Platform runs each organization's agents in the organization's namespace, which is created at run time and labeled `giantswarm.io/organization`, and each agent's atespace is its namespace. The provider read its grants from a fixed list in the chart, so no organization namespace was granted and its agents' model keys, MCP headers and git credentials were refused, and the golden actors of their templates, which boot in `ate-golden`, could not fetch their git skills ([giantswarm/agent-platform#1041](https://github.com/giantswarm/agent-platform/issues/1041)).

## History

to file — prepared in the fork: branch `upstream/credential-provider-namespace-selector`

[#456](https://github.com/giantswarm/substrate/pull/456): the fork's `git cherry-pick -x` of the upstream-ready commit, with the chart README and values rows resolved against the line's

2026-10-20: giantswarm/substrate-credential-providers#13 (merged 2026-10-19, `4c269d06`) ports the same grants to the `google-access-token` plugin only, while the bundled Kubernetes provider keeps this patch, so the outcome stays to-file.

2026-10-20: filed upstream as [agent-substrate/substrate#2877](https://github.com/agent-substrate/substrate/pull/2877) (opened 2026-10-19, the `ownNamespaceSelector` policy) and [agent-substrate/substrate#2959](https://github.com/agent-substrate/substrate/pull/2959) (opened 2026-10-20, the policy reload without a restart that #2877's review asked for), so the outcome moves from to-file to open.

2026-10-20: decided, in the fork-reduction review of this theme, after EItanya's review of agent-substrate/substrate#2877 (reload first, the selector too specific, the RBAC shape to a security reviewer, the provider API the path for needs the bundled provider does not meet): the `ate-golden` half leaves at the re-pin past agent-substrate/substrate#2543, the tenant-grant half waits for upstream's direction on agent-substrate/substrate#2876 with an out-of-tree provider as the fallback dated 2026-11-17; `tenants.organizations.enabled` stays on internal installations until the #2543 re-pin.
