---
id: wait-for-socket-test-bounds-poll-interval
fork: substrate
theme: test-flakes
category: test
status: carried
commits:
  - 'ateom-microvm: bound waitForSocket''s poll interval, not the wall clock'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-19
---
# waitForSocket's promptness test bounds the poll interval, not the wall clock

## Change

`TestWaitForSocketReturnsPromptly` (`cmd/ateom-microvm/internal/kata`) bounds `socketPollInterval` at 5 ms instead of the whole wait's wall-clock time at 40 ms, and still fails when the helper goes back to its 50 ms tick. `TestWaitForSocketAppearsLater` covers a socket that appears after the wait starts, without a wall-clock bound. Test-only.

## Reason

The wall-clock bound measured the runner's scheduling latency, not the helper: the root-gated `run-tests` job failed on it at 0.12 s ([#407](https://github.com/giantswarm/substrate/issues/407)) while the helper polled every millisecond. Under three CPU hogs per core the old test failed 42 of 500 runs (69–86 ms), the new ones none of 500.

## History

not opened yet: the upstream-ready commit is on the branch `upstream/wait-for-socket-poll-bound`, on the mirror `main` at the pin (v0.5.0-alpha2)

[#408](https://github.com/giantswarm/substrate/pull/408): the fork's own commit, a `git cherry-pick -x` of the upstream-ready one
