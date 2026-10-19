---
id: assignment-names-selector-no-worker-matches
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: small upstream PR
depends_on: [fail-paused-actors-resume-fast-when-node]
commit: giantswarm/substrate#395
verified: 2026-10-19
---
# An assignment names the selector when no worker matches it

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/assign-selector-mismatch`, on the mirror `main` at the pin (v0.5.0-alpha2)
- **Our commit:** [#395](https://github.com/giantswarm/substrate/pull/395): the fork's own commit, a `git cherry-pick -x` of the upstream-ready one, its refusal folded into the line's `noFreeWorkerError`

## Change

`Schedule` counts the workers whose labels the template's and the actor's worker selectors match, in any state and of any sandbox class. When the fleet has workers and none of them matches, it returns `NoMatchingWorkerError` (still `ErrNoCapacity` through `errors.Is`), and the assignment answers `ResourceExhausted` with `no worker matches the selector "<selector>" (none of the <n> worker(s) carries its labels)`. Matching workers that are all full, and an empty fleet, keep `no worker has room for the actor`. A node restriction (a `PAUSED` actor's local snapshot, [`fail-paused-actors-resume-fast-when-node.md`](fail-paused-actors-resume-fast-when-node.md)) keeps its own node-naming answers, which come first. Only the message changes: the status, the router's parking and the scheduler metric's outcome stay those of a full pool.

## Reason

A WorkerPool label rename left no worker matching an agent template's selector, and the resume answered "no worker has room for the actor", which sent the investigation to capacity first ([#393](https://github.com/giantswarm/substrate/issues/393), [#394](https://github.com/giantswarm/substrate/issues/394)).
