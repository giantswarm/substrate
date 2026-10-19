---
id: lines-ci-pulls-every-docker-hub-image
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): pull Docker Hub images through mirror.gcr.io'
upstream:
  outcome: fork-only
  refs: []
  exit: none
depends_on: []
verified: 2026-10-19
---
# The line's CI pulls every Docker Hub image through `mirror.gcr.io`, Google's public pull-through …

## Change

The line's CI pulls every Docker Hub image through `mirror.gcr.io`, Google's public pull-through cache of Docker Hub: `hack/create-kind-cluster.sh` takes `DOCKER_HUB_MIRROR` (the host pulls the registry, the KVM probe's busybox and the node image — kind's own default, read from the kind module, or `KIND_NODE_IMAGE` — from it under their Docker Hub names, so docker and kind find them locally; every node's containerd resolves `docker.io` through it, Docker Hub as the fallback, so the install's postgres, rustfs, prometheus, otel collector, jaeger and socat pulls go the same way; unset, the script is unchanged), `run-tests` sets `TESTCONTAINERS_HUB_IMAGE_NAME_PREFIX` for the Postgres test containers and installs the Weaver release `hack/verify/metrics.sh` pins, checksum-verified, instead of pulling its image, and both e2e jobs set `DOCKER_HUB_MIRROR`

## Reason

GitHub-hosted runners share IPs that exhaust Docker Hub's unauthenticated pull limit: `run-tests` failed every Postgres-backed test on the pull of `postgres:18-alpine` and both e2e workflows stopped at the kind registry's `registry:3` with `toomanyrequests` ([#241](https://github.com/giantswarm/substrate/issues/241)); the kagent line fixed the same class the same way ([giantswarm/kagent-upstream#340](https://github.com/giantswarm/kagent-upstream/pull/340))

## History

fork-only, never upstream: upstream's CI runs on its own runners and never meets the limit; the script's knobs are unset there and change nothing

[#242](https://github.com/giantswarm/substrate/pull/242) (`ci(fork): pull Docker Hub images through mirror.gcr.io`)
