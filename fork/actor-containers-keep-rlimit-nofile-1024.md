# Actor containers keep RLIMIT_NOFILE at 1024

- **Status:** carried
- **Upstream:** none: upstream raised the limit with no compatibility before its 1.0; the general fix (a restore under the limit its checkpoint was taken with) is to file
- **Our commit:** [#353](https://github.com/giantswarm/substrate/pull/353) (`fix(ocispec): keep RLIMIT_NOFILE at 1024 so earlier checkpoints restore`)

## Change

`ocispec.Build` sets `RLIMIT_NOFILE` to `ocispec.NoFileLimit`, 1024 soft and hard, instead of v0.5.0-alpha2's 1048576; a unit test pins it.

## Reason

runsc refuses a restore whose spec carries other `Rlimits` than the checkpoint's ("Rlimits does not match across checkpoint restore"). With the raised limit, every golden snapshot and every paused actor checkpointed by a 1.8 release crashed on its first restore after the step, and a new conversation from a template's golden failed until the golden was taken again. Raising the limit needs a restore that writes the checkpoint's own limit into the spec, so checkpoints from both sides of the change restore.
