---
id: microvm-restore-discards-cleanup-untar-error
fork: substrate
theme: session-lifecycle
category: compat
status: carried
commits:
  - 'fix: explicitly discard untar error during restore cleanup'
upstream:
  outcome: to-file
  refs: []
  exit: upstream explicitly discards the cleanup receive's error
depends_on: []
verified: 2026-11-09
---
# Explicitly discard the restore cleanup's untar error

## Change

The deferred cleanup in `cmd/ateom-microvm/restore.go` explicitly discards
the error received from `untarDone` when an earlier restore step fails.
It still waits for the untar goroutine to finish before returning.

## Reason

The error check requires the discarded error to be explicit. Cleanup preserves
the restore step's original error while preventing a concurrent untar from
outliving the failed restore.

## History

[The dependency update](https://github.com/giantswarm/substrate/pull/569)
carries the cleanup compatibility fix alongside the x/net update. The ledger
check requires its commit subject to be recorded separately from the exempt
dependency commits.
