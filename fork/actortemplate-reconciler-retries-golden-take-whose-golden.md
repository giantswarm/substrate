# The ActorTemplate reconciler retries a golden take whose golden actor it finds `DELETING`, …

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2483](https://github.com/agent-substrate/substrate/pull/2483) (ours, open; from branch `upstream/golden-stray-state-retry` on upstream `main` @ `c4129c94`): the release of the stray golden actor and the requeue; the clearing of a failure an earlier release recorded stays fork-only, since upstream asks for no fallbacks for old state before v1.0.0
- **Our commit:** [#323](https://github.com/giantswarm/substrate/pull/323): the fork's own commit, on top of the row above

## Change

The ActorTemplate reconciler retries a golden take whose golden actor it finds `DELETING`, `PAUSED`, `PAUSING` or `REVERTING` (`releaseStrayGoldenActor`: `DeleteActor` with `any_state`, which also finishes a cut delete, and a requeue with the work queue's backoff, capped at a minute, recording nothing; the recorded golden stays the template's until the new one is complete; a `GoldenActorUnexpectedState` error recorded by a release before the retry is cleared on the next pass and the golden taken again)

## Reason

a Substrate upgrade that helm-controller rolled back several times cut the retry row's delete of transiently crashed golden actors: the next pass found the golden actor `DELETING` and failed the template with `GoldenActorUnexpectedState`, terminal, so 11 of 19 templates on a production installation kept the golden of the previous step with no retake running, and the next layout change or the trust-bundle re-take ([#316](https://github.com/giantswarm/substrate/issues/316)) would find them stuck ([#321](https://github.com/giantswarm/substrate/issues/321)). Now the take converges after the roll settles, and rolling the release with the retry heals the templates an earlier step failed, with no operator action
