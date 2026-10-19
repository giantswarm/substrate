---
id: ateom-reports-workload-container-exit
fork: substrate
theme: new-rpcs
category: feature
status: carried
commits:
  - 'fix(ateom): report a container exit under a running sandbox'
upstream:
  outcome: to-file
  refs: []
  exit: same as ReportActorExit
depends_on: [ateom-reports-sandbox-exit-of-running-actor]
verified: 2026-10-19
---
# ateom reports an application container that exits under a running sandbox

## Change

ateom-gvisor runs `runsc wait` on every application container of an actor once its boot or restore records the
session, and ends those waits when the session's watch is released. A container that exits while the sandbox keeps
running is recorded with its exit status, and the sandbox watch's next sweep reports it like a sandbox exit: the
actor's traffic is cut and the control plane crashes the actor with the message `sandbox exited while the actor was
running: container "<name>" exited with status <n> while the sandbox kept running` (or `was killed by signal <n>
(<SIG>)`). An exit a lifecycle RPC causes (a checkpoint stops the containers) is not reported, under the same rule
as a sandbox exit.

## Reason

The pause container keeps a gVisor sandbox running after its application process ends, so the sandbox watch never
saw such an exit: the actor stayed RUNNING with nothing in it, its sockets closed with the process, and every turn
waited out its caller's deadline. A restored actor's process ended 83 ms after a model response reached its
connection, and the turn hung until the client's bound; the exit status now names the cause.

## History

to file — prepared in the fork: branch `upstream/container-exit`, on top of `upstream/sandbox-exit-report`, which it extends

[#464](https://github.com/giantswarm/substrate/pull/464)
