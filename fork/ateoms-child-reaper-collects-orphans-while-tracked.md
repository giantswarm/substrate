---
id: ateoms-child-reaper-collects-orphans-while-tracked
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2180]
  exit: upstream PR or as#2180
depends_on: []
commit: giantswarm/substrate#133
verified: 2026-10-19
---
# ateom's child reaper collects orphans while tracked runsc subcommands run

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch `upstream/childreap-tracked-subprocesses` on the mirror `main`, the root-container shape of agent-substrate/substrate#2180 (open)
- **Our commit:** `release-1.3`: [#133](https://github.com/giantswarm/substrate/pull/133) (first release 1.3.1); `giantswarm`: [#135](https://github.com/giantswarm/substrate/pull/135); also [#186](https://github.com/giantswarm/substrate/pull/186) (`fix(childreap): share the tracked PIDs across every reaper`)

## Change

ateom's child reaper collects orphans while tracked runsc subcommands run (`internal/childreap`: `RunCommand` and `CombinedOutput` start their subprocess under a process-wide lock and record its PID in a set every `Reaper` of the process skips, so a second reaper or one finishing its last round never takes a caller's exit status; a reap collects every other child, listed from the tasks' `children` files or a `/proc` scan, also while tracked subprocesses run; `Enter` keeps its exclusion for subprocesses the reaper cannot see)

## Reason

the reaper held off all reaping while any runsc subcommand ran, and `runsc delete` of the root container polls `kill(pid, 0)` until the SIGKILLed sandbox is gone; its zombie answers signal 0, so the delete waited until ateom's 30 s cleanup deadline killed it, and every checkpoint of an actor restored from a 1.0.x golden took over 30 s, past kagent's quiesce deadline ([giantswarm/substrate#124](https://github.com/giantswarm/substrate/issues/124))
