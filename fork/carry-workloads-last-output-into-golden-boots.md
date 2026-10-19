---
id: carry-workloads-last-output-into-golden-boots
fork: substrate
theme: golden-boot
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: trim the branch, then upstream PR
depends_on: []
commit: cea85000
verified: 2026-10-19
---
# Carry the workload's last output into the golden boot's failure

- **Status:** carried
- **Upstream:** to file, output-tail half only: the branch [`upstream/golden-boot-bound`](https://github.com/giantswarm/substrate/tree/upstream/golden-boot-bound) (`6a9e73f6`) still carries the dropped bound and needs trimming first. Nothing upstream carries the workload's output (checked v0.4.0-alpha1)
- **Our commit:** `cea85000` (the #39 original `4becc5cd`, first release v0.0.30-gs.4; re-pinned onto v0.3.0-alpha1 without the bound)

## Change

Carry the workload's last output into the golden boot's failure (ateom keeps each application container's last output lines, `actorlog.OutputTail`, 12 lines cut at 512 bytes, fed by the log pipe both runtimes run, and the wakeup probe quotes them in its deadline error: `wakeup probe for "kagent" never returned 200 within 30s ...; last output of "kagent":` and the lines; the micro-VM runtime starts its log forwarding before the wakeup wait). The boot bound itself (`GoldenSnapshotStatus.workload_boot_failures`, `GoldenActorNotReady`) is not carried: upstream crashes the actor on the first failed atelet Run (agent-substrate/substrate#1220, in v0.3.0-alpha1), the reconciler fails the template with the recorded crash, and kagent bounds the retries (`golden boot N of 6`)

## Reason

a golden boot whose workload exits before its readiness probe answers is not a crash: ateom returned the missed deadline, the golden actor stayed `RESUMING` on its claimed worker, the reconciler retried, and the same boot ran once a minute for as long as the template existed with `error_message` never set — kagent reported `Ready=False ActorTemplatePending` indefinitely and the workload's own last lines (`fatal: could not read Username for 'https://github.com'`, the Go ADK's `failed to materialize Agent Plugins … exit status 128`) were only in the worker pod's log, which the agent's author cannot read. On an installation (2026-09-15) a portal-created agent with three private skills and no credential looped about 120 boots over two hours until its HelmRelease was patched by hand. Now the crash message on the template and the Agent's `ActorTemplateFailed` condition quote the workload's last lines
