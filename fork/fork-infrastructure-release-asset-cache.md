---
id: fork-infrastructure-release-asset-cache
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): cache and retry release asset downloads'
upstream:
  outcome: fork-only
  refs: [giantswarm/substrate#577]
  exit: Remove when the fork no longer owns these CI workflows.
depends_on: []
verified: 2026-11-11
---
# CI release asset caches

## Change

Cache Weaver and helm-unittest archives and checksums by version and runner
platform, and the installed envtest control plane by version selector and tool
lockfile. Retry cold downloads with bounded backoff; verify archive checksums
on both cold and warm runs. Publish a downloaded file only after curl succeeds.

## Reason

Transient release-download failures must not invalidate cached tools or hold
unrelated pull requests. The fork owns these workflow setup steps.
