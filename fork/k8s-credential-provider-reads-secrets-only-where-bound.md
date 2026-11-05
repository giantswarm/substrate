---
id: k8s-credential-provider-reads-secrets-only-where-bound
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(chart): bind the credential provider''s Secret reader by namespace'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#131]
  exit: "kagent-dev/substrate#131 merged, then the next re-pin drops it"
depends_on: []
verified: 2026-11-05
---
# The credential provider reads Secrets only where it is bound

## Change

`credentialProvider.rbac.namespaces`, the convention of kagent-dev/kagent's chart (`rbac.namespaces`). Empty (the default), the provider's Secret reader ClusterRole stays bound cluster-wide. Set, the chart binds it by RoleBinding in each listed namespace only; a namespace created later is bound by whoever creates it.

## Reason

The provider reads one Secret per request, in the namespace its URI names (`cmd/credential-provider/kubernetes-secrets/kubeprovider.go`), and returns `PermissionDenied` on a 403, so it needs no cluster-wide grant. Bound cluster-wide on a management cluster, it can read every workload cluster's admin kubeconfig.

## History

Upstream code needs no change. agent-substrate/substrate's example manifest says a production deployment would scope this grant (`manifests/egress-credential-injection/k8s-credential-provider.yaml`, "POC scope note"). The chart is kagent-dev/substrate's, so the upstream pull request goes there: kagent-dev/substrate#131, the same change.

`0e04b70c` ([#554](https://github.com/giantswarm/substrate/pull/554))
