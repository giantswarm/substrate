---
id: ateom-fails-boot-sandbox-did-not-take-network
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
commit: giantswarm/substrate#418
verified: 2026-10-19
---
# Fail a boot whose sandbox did not take the actor's network, and retake its golden

- **Status:** carried
- **Upstream:** to file: the branch [`upstream/sandbox-network-taken`](https://github.com/giantswarm/substrate/tree/upstream/sandbox-network-taken) (`3f05e24c`, on v0.5.0-alpha2) carries the ateom half; the transient cause is the line's own, beside its other transient crash causes
- **Our commit:** [#418](https://github.com/giantswarm/substrate/pull/418): a `cherry-pick -x` of the upstream-ready commit and the line's transient cause

## Change

ateom-gvisor checks right after the pause container starts or is restored that the sandbox took the actor's network: gVisor moves the addresses of every interface in the actor's namespace into its own stack, so a kernel that still carries 169.254.17.2 means the sandbox has no interface. Such a boot fails at once with `the sandbox did not take the actor's network: eth0 in its namespace still carries 169.254.17.2`, and a missed wakeup probe states whether the sandbox holds the actor's network. ate-api names that cause among the transient crash causes, so the template reconciler takes the golden again and a resume that crashed for it returns the actor to its snapshot.

## Reason

a sandbox without the actor's interface leaves the kernel holding the address, and the kernel resets every connection to it: the wakeup probe was refused for its whole 30 s while the workload's own output said it was ready, and the template failed for good with `GoldenActorCrashed` ([#398](https://github.com/giantswarm/substrate/issues/398), helm-e2e on `395070a5`, gVisor lane). The failure named neither the network nor the worker, and a retry would have outlived it.
