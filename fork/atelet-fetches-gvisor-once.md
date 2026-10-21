---
id: atelet-fetches-gvisor-once
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(atelet): fetch an uncached gVisor release once per node, flushed to disk'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-21
---
# atelet fetches an uncached gVisor release once per node

## Change

`fetchGVisorRelease` and `fetchAsset` (`cmd/atelet/sandbox_assets.go`) run their cache miss in a single flight per sha256
(`AteomHerder.assetFetches`): a caller that misses the cache while a fetch of the same asset is in flight waits for that
fetch's result. The shared fetch runs detached from the caller that started it, bounded by `assetFetchTimeout` (15
minutes); each caller stops waiting on its own context. The downloaded tarball and every extracted file are `fsync`ed
before close, and the zstd decoder runs with one goroutine and low-memory buffers.

## Reason

A gVisor nightly is a 123 MiB tarball with a 128 MiB zstd window that inflates to 311 MiB. Two callers that missed the
cache at once (the sandbox-asset prewarm and a workload, or two workloads) each downloaded and extracted it; the
unflushed writes stay dirty page cache charged to atelet's cgroup, which is not reclaimable until written back. Measured
against a cgroup on the 2026-09-09 nightly, two concurrent first users: 1179 MiB peak with 311 MiB dirty before, 591 MiB
peak with none dirty after. atelet's limit is 1Gi; it was OOM-killed on a test installation stepping to 1.17.5-rc.3, the
release that moves the `gvisor-default` SandboxConfig to that nightly, failing the in-flight Run, Restore and Checkpoint
calls.

## History

not opened yet: the upstream-ready change is on the branch `upstream/atelet-gvisor-extract-memory` (`429a9340`, mirror
`main` @ `e2fc0fa9`, DCO-signed): the `fsync`s and the zstd decoder options only. `main` already fetches each asset once
at a time through its `filecache.Store` single flight, so the branch carries no single flight of its own.

[#533](https://github.com/giantswarm/substrate/pull/533), for [#532](https://github.com/giantswarm/substrate/issues/532)
