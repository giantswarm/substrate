---
id: bundled-rustfs-deployment-stops-old-pod-before
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: kd chart PR
depends_on: []
commit: bff94a57
verified: 2026-10-19
---
# The bundled RustFS Deployment stops its old pod before it starts the new one

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/rustfs-recreate`](https://github.com/giantswarm/substrate/tree/upstream/rustfs-recreate) here (`fbcefb79` the template hunk, `8634c2bf` the rendered `manifests/ate-install/rustfs.yaml`, `0d834268` the comment's per-node wording, `e10b45af` no surge instead of `Recreate`, on the mirror `main` @ `8316538f`, DCO-signed, no fork references; the first message is the pull-request text; the chart test stays fork-only, upstream has no chart tests), for kagent-dev/substrate; a team member opens it with DCO sign-off once the team has reviewed it
- **Our commit:** `bff94a57` ([#76](https://github.com/giantswarm/substrate/pull/76), squash: `Recreate`) and `744bd4d7` ([#83](https://github.com/giantswarm/substrate/pull/83), no surge instead)

## Change

The bundled RustFS Deployment stops its old pod before it starts the new one (`RollingUpdate` with `maxSurge: 0` and `maxUnavailable: 1`)

## Reason

it runs one replica on the ReadWriteOnce claim `rustfs-data`, and the default RollingUpdate starts the new pod while the old one still holds the volume, which ReadWriteOnce attaches to one node: a new pod scheduled on another node stays `ContainerCreating` (`FailedAttachVolume`, the volume already used by the old pod), the old one is never scaled down, and the Substrate release waiting on the rollout stalls until the old pod is deleted by hand (an installation's upgrade). Not `Recreate`, which the patch first carried: an existing Deployment keeps the API server's defaulted `rollingUpdate`, which no field manager owns, so helm-controller's server-side apply of `type: Recreate` cannot drop it and the API server refuses the upgrade (`spec.strategy.rollingUpdate: Forbidden: may not be specified when strategy type is 'Recreate'`, a server-side dry-run over an installation's 1.1.0 Deployment; an explicit `rollingUpdate: null` is refused the same way). No surge and one unavailable apply over the defaulted strategy and let a rollout already stuck with two pods finish. The bundled Postgres is a StatefulSet and replaces its pod delete-first. `charts/substrate/tests/rustfs_test.yaml` holds the strategy
