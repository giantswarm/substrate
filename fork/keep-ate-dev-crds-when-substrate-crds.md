---
id: keep-ate-dev-crds-when-substrate-crds
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - Keep the CRDs when the substrate-crds release is uninstalled
upstream:
  outcome: to-file
  refs: [giantswarm/substrate#12]
  exit: kd chart PR
depends_on: []
verified: 2026-10-20
---
# Keep the `ate.dev` CRDs when the `substrate-crds` release is uninstalled

## Change

Keep the `ate.dev` CRDs when the `substrate-crds` release is uninstalled (`helm.sh/resource-policy: keep` on the three CRD templates, set at the generator as a `+kubebuilder:metadata:annotations` marker on the root types, so `hack/verify/crd-chart.sh` keeps the templates a verbatim copy)

## Reason

the chart ships its CRDs as templates, so an uninstall of that release deleted the CRDs and every `WorkerPool`, `SandboxConfig` and `CSIDriverConfig` with them; a GitOps controller that uninstalls the releases concurrently — a cluster's own Flux finalizing the meta chart's component `HelmRelease`s — can remove the CRDs first, and the `substrate` release's uninstall then fails for good on its `SandboxConfig` (`failed to delete release: substrate`, Helm cannot delete an object whose kind is gone; [giantswarm/agent-platform#385](https://github.com/giantswarm/agent-platform/issues/385)). The kagent line carries the same policy on `kagent-crds` (giantswarm/kagent-upstream#10). A kept CRD means its objects survive a Substrate uninstall: the consumer deletes its `WorkerPool`s and `SandboxConfig`s, or the CRDs explicitly

## History

to file: the upstream-shaped patch is branch [`upstream/substrate-crds-resource-policy-keep`](https://github.com/giantswarm/substrate/tree/upstream/substrate-crds-resource-policy-keep) here (`41bd9df0`, on the mirror `main`; the pull-request text is in #12); neither kagent-dev/substrate nor agent-substrate/substrate carries or proposes the policy (searched 2026-09-11); a team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it

`afdacaca` (as it is on `giantswarm` after the v0.0.29 re-pin), [#12](https://github.com/giantswarm/substrate/pull/12)

2026-10-20: [#12](https://github.com/giantswarm/substrate/pull/12) (merged 2026-09-11) is the line's own pull request for this patch, not an upstream exit, so the outcome stays to-file.
