---
id: charts-docker-hub-image-defaults-name-their
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: kd chart PR
depends_on: []
commit: 735555b1
verified: 2026-10-19
---
# The chart's Docker Hub image defaults name their registry

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/qualified-image-refs`](https://github.com/giantswarm/substrate/tree/upstream/qualified-image-refs) here (`27ae1f9`, on the mirror `main`); upstream's `manifests/ate-install/**` carry the same short names (rustfs, aws-cli, postgres; in `kind/` prometheus, otel, jaeger, envoy) and should travel with it; `images.awsCli` moved to the gsoci copy with the hook row below
- **Our commit:** `735555b1` ([#42](https://github.com/giantswarm/substrate/pull/42)), [#43](https://github.com/giantswarm/substrate/pull/43)

## Change

The chart's Docker Hub image defaults name their registry (`images.postgres`, `images.rustfs`, `images.busybox` → `docker.io/library/postgres:…`, `docker.io/rustfs/rustfs:…`, `docker.io/library/busybox:1.36`; tags and digests unchanged). `images.awsCli` stays a short name: it is the `rustfs-bucket-init` Job's image and a Job's pod template is immutable, so a change fails every existing installation's upgrade with `field is immutable` until the completed Job is deleted by hand — #42 qualified it, [#43](https://github.com/giantswarm/substrate/pull/43) took that back; it moves with a change that lets the Job be recreated (a hook Job, or a template-hash name — the platform's network policies select it by `job-name`)

## Reason

which registry a short name means is the node runtime's call, and only containerd defaults it to docker.io — CRI-O (short-name mode enforcing, the containers-common alias table only) refuses `rustfs/rustfs`, `postgres` and `amazon/aws-cli` with `ImageInspectError: … returns ambiguous list`. On a bare-metal installation (containerd Sparks, CRI-O control planes, no taints) a drain rescheduled rustfs onto a CRI-O node on 2026-09-17 and the snapshot store was gone for 25 min; the bundled postgres and the bucket-init Job carry the same exposure. The installation overrides `substrate.images.*` in the meta chart until a release of the line ships this. On the line the `docker.io` names are superseded by the gsoci copies (next row); this row's upstream-shaped form is what upstream gets
