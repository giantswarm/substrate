---
id: gvisor-sandboxconfigs-pause-image-chart-value
fork: substrate
theme: chart-knobs
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/substrate#23]
  exit: kd chart PR
depends_on: []
commit: f844d726
verified: 2026-10-19
---
# The gVisor SandboxConfig's pause image is a chart value

- **Status:** carried
- **Upstream:** to file (prepared): branch [`upstream/sandbox-pause-image-value`](https://github.com/giantswarm/substrate/tree/upstream/sandbox-pause-image-value) here (`1591d20d`, the same commit on kagent-dev `main` @ `9c4b1fb5` of 2026-10-08, DCO signed), the shape of [kagent-dev/substrate#23](https://github.com/kagent-dev/substrate/pull/23) (2026-08-20, closed unmerged 2026-10-06 on a stale base)
- **Our commit:** `f844d726` ([#225](https://github.com/giantswarm/substrate/pull/225), rebase-merged 2026-10-09; first release 1.6.3)

## Change

The gVisor SandboxConfig's pause image is a chart value (`images.pause`, default upstream's `registry.k8s.io/pause:3.10.2@sha256:f548e0e8…`, rendered with `required`; the unit-test suite `charts/substrate/tests/sandboxconfig_gvisor_test.yaml` covers the default, an override and the empty value; the README row; the preserved kubectl-apply manifest keeps upstream's default)

## Reason

the one image reference the chart hardcoded, and the one an operator could not move: `registry.k8s.io` redirects every pull to a Google Artifact Registry host, so a cluster whose egress admits only its own registry failed every golden boot while creating the pause OCI bundle, before any snapshot existed ([#224](https://github.com/giantswarm/substrate/issues/224))
