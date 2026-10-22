---
id: ateom-answers-repeated-checkpoint
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateom): answer a repeated full checkpoint with the one taken'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-21
---
# ateom answers a repeated full checkpoint with the one taken

## Change

ateom-gvisor remembers each actor's last completed full checkpoint (`cmd/ateom-gvisor/checkpointed.go`): its checkpoint
directory and the answer it gave. A `CheckpointWorkload` of full scope for an actor it no longer hosts is answered that
checkpoint while exactly the files it reported are still in that directory; otherwise it runs as before. The record is
dropped when the actor runs, restores or terminates on the worker again.

## Reason

A full checkpoint stops the sandbox. When the atelet that asked for it restarts before it answers (a rolling atelet
DaemonSet), ate-api retries the suspend through the new atelet, whose `CheckpointWorkload` finds no sandbox: `runsc
checkpoint` exits 128, the actor is crashed with a cause no retry outlives, and the snapshot already written is never
uploaded. A conversation loses the turns since its last snapshot; a golden retake fails its template for good, so new
conversations keep starting from the golden of the older layout. Seen on a test installation during the roll to
1.17.5-rc.2: the first checkpoint completed, the retry four seconds later crashed the golden actor.

## History

not opened yet: the change is self-contained in ateom-gvisor and applies to upstream as is.

[#531](https://github.com/giantswarm/substrate/pull/531)
