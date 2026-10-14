# ateom reports the sandbox exit of a running actor

- **Status:** carried
- **Upstream:** not opened yet, row 192 of the upstream queue ([giantswarm/giantswarm#38476](https://github.com/giantswarm/giantswarm/issues/38476)): the upstream-ready commit is on the branch `upstream/sandbox-exit-report` (`1a8c98f7`, mirror `main` @ `5cf1c245` + the change, DCO-signed; `main`'s `crashActor` only frees the worker, so the upstream commit does not tear the sandbox down)
- **Our commit:** [#439](https://github.com/giantswarm/substrate/pull/439): the fork's own commit of the same change on the pin, whose crash also terminates the sandbox on the worker

## Change

ateom-gvisor watches the sandbox process of every actor it hosts through a pidfd on the pid runsc writes for the
root container, swept every second. A sandbox that exits while no lifecycle RPC holds the actor (a checkpoint or a
terminate stops it under the actor's lock and unhosts it) has its actor's traffic cut at once and is reported to
atelet's new `AteomSupport.ReportActorExit`, which forwards it to the control plane's new
`WorkerService.ReportActorExit`. ate-api-server crashes the actor under its lease when it is the RUNNING
incarnation assigned to the reporting worker, with the message `sandbox exited while the actor was running:
sandbox process <pid> exited`, tearing down what is left on the worker and freeing it, as for any crash. A boot or
restore whose sandbox is gone before the watch starts fails, which records the crash it is.

## Reason

Nothing observed a running actor's sandbox: one killed right after ate-api-server recorded RUNNING (10 ms later in
the reproduction) left the actor RUNNING for minutes with no crash, and every turn sent to it hung until the
client's deadline. kagent's recovery (revert and resume) starts only from a crash
([#437](https://github.com/giantswarm/substrate/issues/437)).
