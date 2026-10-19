---
id: pause-image-defaults-gsoci-copy-images-pause
fork: substrate
theme: chart-knobs
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: adopt global.imageRegistry
depends_on: [gvisor-sandboxconfigs-pause-image-chart-value]
commit: dd957c2e
verified: 2026-10-19
---
# The pause image defaults to its gsoci copy

- **Status:** carried
- **Upstream:** **ours to keep**: a Giant Swarm registry is not upstream's default; the upstream-shaped change is the row above
- **Our commit:** `dd957c2e` ([#225](https://github.com/giantswarm/substrate/pull/225), rebase-merged 2026-10-09; first release 1.6.3)

## Change

The pause image defaults to its gsoci copy: `images.pause` = `gsoci.azurecr.io/giantswarm/pause:3.10.2@sha256:f548e0e8…`, upstream's digest

## Reason

every Giant Swarm installation pulls from gsoci.azurecr.io, and one whose egress admits only that registry cannot reach `registry.k8s.io`; the same digest, so the snapshots that record it restore unchanged ([#224](https://github.com/giantswarm/substrate/issues/224))
