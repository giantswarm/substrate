---
id: actor-containers-keep-rlimit-nofile-1024
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ocispec): keep RLIMIT_NOFILE at 1024 so earlier checkpoints restore'
upstream:
  outcome: declined
  refs: [agent-substrate/substrate#2530]
  exit: 'dropped at the conversion re-pin: no checkpoint taken before it warm-restores across the gVisor fs/ and rootfs/ layout, so the old limit protects nothing'
  class: 1
  destination: ''
  first_step: 'drop at the conversion re-pin (giantswarm/substrate)'
  source: 'giantswarm @ 2e90412b6'
  test: 'the conversion lab upgrade: a paused gVisor actor from 1.16.3 cold-boots with its files under RLIMIT_NOFILE 1048576'
  recheck: '2026-11-03, with the conversion re-pin'
  owner: unassigned
  draft: issuecomment-6301320155
  decision: issuecomment-6301319662
  couplings: [compat-migrations]
  open_question: ''
depends_on: []
verified: 2026-10-21
---
# Actor containers keep RLIMIT_NOFILE at 1024

## Change

`ocispec.Build` sets `RLIMIT_NOFILE` to `ocispec.NoFileLimit`, 1024 soft and hard, instead of v0.5.0-alpha2's 1048576; a unit test pins it.

## Reason

runsc refuses a restore whose spec carries other `Rlimits` than the checkpoint's ("Rlimits does not match across checkpoint restore"). With the raised limit, every golden snapshot and every paused actor checkpointed by a 1.8 release crashed on its first restore after the step, and a new conversation from a template's golden failed until the golden was taken again. Raising the limit needs a restore that writes the checkpoint's own limit into the spec, so checkpoints from both sides of the change restore.

## History

[agent-substrate/substrate#2530](https://github.com/agent-substrate/substrate/issues/2530) (closed 2026-10-13 by BenTheElder: "There's no compatibility guarantee right now … NOT supporting backwards compat right now"); upstream writes no restore under the checkpoint's own limit, so this commit stays on the line

[#353](https://github.com/giantswarm/substrate/pull/353) (`fix(ocispec): keep RLIMIT_NOFILE at 1024 so earlier checkpoints restore`)

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: drop (class 1): the next re-pin past kagent-dev/kagent 96c40300 is the first whose Substrate carries agent-substrate/substrate#2888; the fork converts state across it instead of cutting over, every old gVisor checkpoint cold-boots across the new layout, so the limit drops with it; the restore-validated limit is part of the upstream issue on the snapshot compatibility stamp.
