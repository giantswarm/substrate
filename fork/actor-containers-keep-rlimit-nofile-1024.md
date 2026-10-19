---
id: actor-containers-keep-rlimit-nofile-1024
fork: substrate
theme: compat-migrations
category: compat
status: carried
upstream:
  outcome: declined
  refs: [agent-substrate/substrate#2530]
  exit: none; carried for good unless the line drops pre-1.0 checkpoints
depends_on: []
commit: giantswarm/substrate#353
verified: 2026-10-19
---
# Actor containers keep RLIMIT_NOFILE at 1024

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2530](https://github.com/agent-substrate/substrate/issues/2530) (closed 2026-10-13 by BenTheElder: "There's no compatibility guarantee right now … NOT supporting backwards compat right now"); upstream writes no restore under the checkpoint's own limit, so this commit stays on the line
- **Our commit:** [#353](https://github.com/giantswarm/substrate/pull/353) (`fix(ocispec): keep RLIMIT_NOFILE at 1024 so earlier checkpoints restore`)

## Change

`ocispec.Build` sets `RLIMIT_NOFILE` to `ocispec.NoFileLimit`, 1024 soft and hard, instead of v0.5.0-alpha2's 1048576; a unit test pins it.

## Reason

runsc refuses a restore whose spec carries other `Rlimits` than the checkpoint's ("Rlimits does not match across checkpoint restore"). With the raised limit, every golden snapshot and every paused actor checkpointed by a 1.8 release crashed on its first restore after the step, and a new conversation from a template's golden failed until the golden was taken again. Raising the limit needs a restore that writes the checkpoint's own limit into the spec, so checkpoints from both sides of the change restore.
