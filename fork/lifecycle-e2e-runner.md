---
id: lifecycle-e2e-runner
fork: substrate
theme: test-flakes
category: test
status: carried
commits:
  - 'fix(e2e): wait out a held actor lease in the lifecycle runner'
upstream:
  outcome: superseded
  refs: []
  exit: mark the file dropped
depends_on: []
verified: 2026-10-19
---
# The lifecycle e2e runner

## Change

The lifecycle e2e runner (`TestDurableDirLifecycle`, `TestMultipleDurableDirLifecycle`) paused and suspended through `e2e.AwaitActorLease`, which retried an `Aborted` "another operation is in progress" every second for up to 30 s. Superseded by the row below, which makes the wait unnecessary and removes the helper; the commit stays in the history

## Reason

the durable pause (above) committed its upload under the actor's lease after `PauseActor` returned; on the loaded `E2E (agentgateway)` job the commit outlasted `acquireActorLease`'s retry of about a second, and the pause or suspend right after a resume from `PAUSED` failed with `Aborted` in the `onCommit:Data, onPause:Full` cases, the flake reported as "no free workers available", whose saturation `ResumeActorAwaitCapacity` already waits out ([giantswarm/substrate#159](https://github.com/giantswarm/substrate/issues/159))

## History

test-only, superseded

`giantswarm`: `fix(e2e): wait out a held actor lease in the lifecycle runner` (`2014424b`); also [#188](https://github.com/giantswarm/substrate/pull/188) (`fix(e2e): wait out a held actor lease in the lifecycle runner`)
