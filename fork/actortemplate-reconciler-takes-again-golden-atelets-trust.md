---
id: actortemplate-reconciler-takes-again-golden-atelets-trust
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): take again a golden the trust-bundle guard failed'
upstream:
  outcome: fork-only
  refs: [giantswarm/substrate#317]
  exit: 'folded at the conversion re-pin into the row conversion, which applies it before re-encoding; drops with that conversion at the following re-pin'
  class: 6
  destination: 'giantswarm/substrate ate-api row conversion of the conversion re-pin'
  first_step: 'at the conversion re-pin, the row conversion applies this migration before re-encoding (giantswarm/substrate)'
  source: 'giantswarm @ cfbdad2fd; conversion: new branch'
  test: 'the conversion lab upgrade from Substrate 1.16.3; the conversion counter reports no failed row'
  recheck: 'Substrate GA (agent-substrate/substrate#2004), fallback 2026-11-24, with the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: 'issuecomment-6301319662, amended issuecomment-6339634309'
  couplings: [compat-migrations]
  open_question: ''
depends_on: [giantswarm/substrate#317, store-migrates-actortemplate-row-release-before-upstream]
verified: 2026-10-21
---
# The ActorTemplate reconciler takes again a golden atelet's trust-bundle guard failed

## Change

The ActorTemplate reconciler takes again a golden atelet's trust-bundle guard failed (`retakeableGoldenFailure`, `retakeableGoldenCrash`: a recorded `GoldenActorCrashed` whose cause is `no trust bundle names given` is cleared, a golden actor found crashed on it released with `releaseCrashedGoldenActor`, and the golden taken again, once every trust-bundle projection of the template, as the store reads it, names a bundle; the recorded golden stays the template's until the new one is complete; a template that still names none keeps its failure and a boot the guard refuses fails it as before, so nothing retries a refusal that still holds)

## Reason

a template row a release before upstream #2048 wrote and a later checkpoint rewrote without its name booted with no names under v1.8.3, the guard refused it and the reconciler recorded `GoldenActorCrashed`, terminal, so the read of the rewritten row as its one name (the migration row above) would never reach the golden: on a production installation two golden re-takes of templates from before v1.6.0 stayed failed ([#330](https://github.com/giantswarm/substrate/issues/330)). Now rolling the release heals them with no operator action. Unit tests on the predicates and on the reconciler (red without the clause); `TestGoldenTagHealsRewrittenTrustBundleRow` (Postgres, the reconciler, fake atelet) rewrites a row whole to that shape and proves the golden actor's `Run` carries `egress-mitm.ate.dev` and the golden is taken again

## History

none: the guard's text is the line's own ([#317](https://github.com/giantswarm/substrate/pull/317)), and upstream asks for no fallbacks for old state before v1.0.0; falls away with the migration row above

[#333](https://github.com/giantswarm/substrate/pull/333): the fork's own commit, on top of the migration row's

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: fork-only (class 6): the next re-pin past kagent-dev/kagent 96c40300 is the first whose Substrate carries agent-substrate/substrate#2888; the fork converts state across it instead of cutting over; the row conversion applies this read-migration to every row before re-encoding it, so no legacy row survives, and the commit leaves at that re-pin.

2026-10-22: amended in the fork-reduction review of the compat-migrations theme: the conversion re-pin is the first past Substrate GA (fallback 2026-11-24), with one conversion written then against the frozen format; whether it runs as a one-shot agent-platform Job or as fork code for that one re-pin is decided then.
