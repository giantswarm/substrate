---
id: two-serviceaccounts-take-annotations-ate-api-server
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'helm: ServiceAccount annotations for atelet and ate-api-server, extraEnv for ate-api-server'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1277, kagent-dev/substrate#31]
  exit: kd chart PR for the annotations half; extraEnv half drops
depends_on: []
verified: 2026-10-20
---
# The two ServiceAccounts take annotations and ate-api-server takes `extraEnv`

## Change

The two ServiceAccounts take annotations and ate-api-server takes `extraEnv` (`atelet.serviceAccount.annotations`, `ateApiServer.serviceAccount.annotations`, `ateApiServer.extraEnv`; all empty by default, the rendered manifests unchanged)

## Reason

an installation whose snapshot store is not the bundled RustFS binds a pod identity to the pods that reach it (IRSA `eks.amazonaws.com/role-arn`, GKE Workload Identity `iam.gke.io/gcp-service-account`) and hands ate-api-server the store's endpoint; atelet took `extraEnv`, ate-api-server only the `ate-api-server-envvars` ConfigMap the chart itself creates, and neither ServiceAccount an annotation. The meta chart's snapshot-store block sets all three ([giantswarm/agent-platform#411](https://github.com/giantswarm/agent-platform/issues/411))

## History

to file: the upstream-shaped patch is branch [`upstream/serviceaccount-annotations-extraenv`](https://github.com/giantswarm/substrate/tree/upstream/serviceaccount-annotations-extraenv) here (`c5f2a3d4`, on the mirror `main` @ `007eb1ee`; its message is the pull-request text). No upstream issue or pull request covers it (searched kagent-dev/substrate and agent-substrate/substrate, 2026-09-13: agent-substrate/substrate#1277 documents the GKE Workload Identity grants `setup-gcp` creates out of band, kagent-dev/substrate#31 makes the ServiceAccount names configurable). A team member opens the pull request with DCO sign-off once the team has reviewed it

`4e4b8f27`, [#27](https://github.com/giantswarm/substrate/pull/27) (rebase-merged 2026-09-13; first release v0.0.27-gs.8)

2026-10-20: agent-substrate/substrate#1277 (merged 2026-08-28, `ab12d954`, in the pin) documents the Workload Identity grants and adds no chart knob, so the outcome stays to-file.
