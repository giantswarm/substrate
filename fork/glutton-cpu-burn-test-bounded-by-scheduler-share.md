# The glutton CPU burn test is bounded by the scheduler's share

- **Status:** carried
- **Upstream:** not opened yet, row 195 of the upstream queue ([giantswarm/giantswarm#38476](https://github.com/giantswarm/giantswarm/issues/38476)): the upstream-ready commit is on the branch `upstream/glutton-cpu-time-scheduler-share` (`7c724a01`, mirror `main` @ `5cf1c245` + the change, DCO-signed)
- **Our commit:** [#449](https://github.com/giantswarm/substrate/pull/449): a `git cherry-pick -x` of the upstream-ready commit, its subject in the line's conventional form

## Change

`internal/benchmarking/glutton`'s `TestUseCPUBurnsCPUTime` runs a reference goroutine that spins on its own OS thread
for the test's window and requires the 100%-duty worker's CPU time to reach half of the reference's. Test-only; nothing
that ships changes.

## Reason

The test required half of a 300 ms wall-clock window as the worker's user CPU time. On a loaded machine the scheduler
grants a runnable thread less than that, so the test failed under a loaded `go test ./...` while the worker burned CPU
as intended ([#448](https://github.com/giantswarm/substrate/issues/448)). The reference measures what the scheduler
grants a spinning thread right now; a worker that sleeps still fails.
