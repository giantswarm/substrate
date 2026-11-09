---
id: microvm-restore-discards-cleanup-untar-error
fork: substrate
theme: session-lifecycle
category: compat
status: carried
commits:
  - 'fix: explicitly discard untar error during restore cleanup'
  - 'fix: check the untar error during restore cleanup'
upstream:
  outcome: to-file
  refs: []
  exit: upstream checks the cleanup receive's error without replacing the restore error
depends_on: []
verified: 2026-11-09
---
# Check the restore cleanup's untar error

## Change

The deferred cleanup in `cmd/ateom-microvm/restore.go` checks and logs
the error received from `untarDone` when an earlier restore step fails.
It waits for the untar goroutine to finish before returning the original
restore error.

## Reason

Checking the receive satisfies errcheck without the redundant blank assignment
that staticcheck rejects as S1005. Cleanup preserves the restore step's original
error while preventing a concurrent untar from outliving the failed restore;
the warning keeps a secondary untar failure visible.

## History

[The dependency update](https://github.com/giantswarm/substrate/pull/569)
carries the cleanup compatibility fix alongside the x/net update. The ledger
check requires its commit subject to be recorded separately from the exempt
dependency commits.
