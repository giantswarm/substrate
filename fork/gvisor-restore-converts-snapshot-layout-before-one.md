---
id: gvisor-restore-converts-snapshot-layout-before-one
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateom): convert a pre-per-volume durable snapshot on restore'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2462, agent-substrate/substrate#2472]
  exit: as#2472 decision; expected decline (pre-1.0)
depends_on: [gvisor-restore-refuses-snapshot-layout-before-one]
verified: 2026-10-19
---
# A gVisor restore converts a snapshot in the layout before one tar per durable volume instead of …

## Change

A gVisor restore converts a snapshot in the layout before one tar per durable volume instead of refusing it (`convertLegacyDurableLayout` in `cmd/ateom-gvisor/durable.go`: `tarutil.Split` rewrites `durable-dir.tar` into one `durable-dir-<volume>.tar` per declared volume, tar to tar, all or nothing, `durable-dir.tar` left in place; each volume is then extracted like a current snapshot). The refusal of the row above stays for a snapshot that does not convert — an unreadable archive, an entry outside every declared volume, a hardlink across volumes — now with `FailedPrecondition` and the cause. `tarutil.Split` is exported for the ActorTemplate reconciler

## Reason

the refusal kept an actor from running with an empty `/data`, but every actor suspended on 1.7, and every actor created from a 1.7 golden, still lost its conversation and durable volumes on the step to 1.8, and so did a snapshot reset ([#271](https://github.com/giantswarm/substrate/issues/271)). Now the first resume on 1.8 restores the data and the next checkpoint writes the per-volume layout

## History

[agent-substrate/substrate#2472](https://github.com/agent-substrate/substrate/pull/2472) (ours, open; from branch `upstream/convert-legacy-durable-layout`, the fork commit on top of [agent-substrate/substrate#2462](https://github.com/agent-substrate/substrate/pull/2462)'s, gVisor only). Upstream's `AGENTS.md` asks for no fallbacks for old formats before v1.0.0, so upstream may decline it; then it is carried until a release after 1.0 makes it moot. Falls away with the refusal's row

[#277](https://github.com/giantswarm/substrate/pull/277): the fork's own commit on top of the refusal's; the micro-VM ateom of the line still writes and reads the single `durable-dir.tar` and needs no conversion
