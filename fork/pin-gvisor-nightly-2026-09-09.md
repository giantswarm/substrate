---
id: pin-gvisor-nightly-2026-09-09
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(gvisor): run the 2026-09-09 nightly with the systrap stub race fix'
upstream:
  outcome: drops-at-repin
  refs: []
  exit: upstream pins a gVisor nightly of 2026-09-09 or later
depends_on: []
verified: 2026-10-21
---
# Run the gVisor 2026-09-09 nightly

## Change

The `gvisor-default` SandboxConfig (`charts/substrate/templates/sandboxconfig-gvisor.yaml`,
`manifests/ate-install/sandboxconfig-gvisor.yaml`, and the example in `docs/api-guide.md`) names the gVisor nightly of
2026-09-09 (`release-20260831.0-24-g7c6199801fd2`), with its tarball's sha256 per architecture, in place of the nightly
of 2026-09-02 (`release-20260824.0-120-g727c8c389c36`). runsc refuses a checkpoint image another build wrote, so the
atelet's `restoreRuntime` (`cmd/atelet/sandbox_assets.go`) runs a restore that loads a checkpoint image (every scope but
data) on the gVisor asset set its snapshot manifest recorded, not the SandboxConfig's: a conversation suspended on the
2026-09-02 nightly resumes on it. `internal/snapshotlayout` adds layout 3 (`GVisorNightly20260909`) as `Current`, so
every golden is taken again and new actors start on the new nightly.

## Reason

The 2026-09-02 nightly carries google/gvisor `d119f5cfb8` ("systrap: recover contexts stuck on an unresponsive stub"),
which kills the systrap stub subprocess when the sentry interrupts an application context whose stub thread it cannot
find. It lacks that change's race fix `80bb741691` ("Close exposed race window in stub ThreadID setting") and
`93ed0053d1` ("Make unexpected subprocess kills thread-safe"). A stub thread that is switching a context back can briefly publish no
ThreadID while the context still reads as being worked on. An interrupt in that window (a checkpoint, a signal, a
preemption) logs `Systrap context has no stub thread to interrupt`. It then kills the subprocess, and every task in it
ends with `Unexpected SwitchToApp error: systrap corrupted memory: subprocess died` and a SIGKILL while the sandbox keeps
running. In the kagent e2e on gVisor this failed claude and kagent turns shortly after a restore and at checkpoints
(giantswarm/kagent-upstream#791), with no memory pressure and nothing in the kernel log. The 2026-09-09 nightly is the
first that carries both fixes, 27 commits past the 2026-09-02 one, none of them in gVisor's checkpoint or restore code.

## History

not for upstream as such: changes the pinned gVisor nightly only. Upstream kagent-dev/substrate `main` names a later
nightly (2026-10-17), and the pin falls away at the re-pin onto it.
