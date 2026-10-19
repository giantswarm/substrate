---
id: keep-podcertificate-controller-system-across-uninstall
fork: substrate
theme: chart-shape
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1166]
  exit: chart-shape migration
depends_on: [chart-keeps-lines-shape-on-v0-5-0-alpha2-base]
commit: b73983ae
verified: 2026-10-19
---
# Keep `podcertificate-controller-system` across an uninstall

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/keep-podcert-namespace`](https://github.com/giantswarm/substrate/tree/upstream/keep-podcert-namespace) here (`38a4e3a1`, on the mirror `main` @ `007eb1ee`); a team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it. No upstream issue or pull request covers it (searched kagent-dev/substrate and agent-substrate/substrate, 2026-09-11: agent-substrate#146 and #1166 touch stale CA material in the kind and e2e setups only)
- **Our commit:** `b73983ae`, [#13](https://github.com/giantswarm/substrate/pull/13) (first release v0.0.27-gs.5)

## Change

Keep `podcertificate-controller-system` across an uninstall (`helm.sh/resource-policy: keep` on the chart's `Namespace`; the kubectl-apply manifest and a chart unit test with it)

## Reason

the namespace holds the two CA pools the podcertificate-controller signs from — provisioned into it out of band (upstream `kubectl-ate admin make-ca-pool`; the platform's connectivity bootstrap hook, which keeps them) — while the signers' `ClusterTrustBundle`s are cluster-scoped and outlive the release. An uninstall took the pools and left the bundles; the reinstall minted new roots, the controller republished the bundles within seconds, but every pod had already read the surviving bundle when it started (`ateapiauth` loads the ate-api CA file once, at dial time) and failed each handshake against ate-api-server with `x509: certificate signed by unknown authority` — no golden boot (the agent-platform ATS own-Flux scenario on the cluster its smoke had uninstalled from, [giantswarm/agent-platform#384](https://github.com/giantswarm/agent-platform/issues/384)). Kept, a reinstall signs from the roots the bundles already carry, as `ate-system`'s pools do (`createNamespace: false`)
