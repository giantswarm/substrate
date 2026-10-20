---
id: conversation-actor-resume-left-crashed-atelets-trust
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): resume an actor the trust-bundle guard crashed'
upstream:
  outcome: fork-only
  refs: []
  exit: 'folded at the conversion re-pin into the row conversion, which applies it before re-encoding; drops with that conversion at the following re-pin'
  class: 6
  destination: 'giantswarm/substrate ate-api row conversion of the conversion re-pin'
  first_step: 'at the conversion re-pin, the row conversion applies this migration before re-encoding (giantswarm/substrate)'
  source: 'giantswarm @ 15f5705b4; conversion: new branch'
  test: 'the conversion lab upgrade from Substrate 1.16.3; the conversion counter reports no failed row'
  recheck: '2026-11-03, with the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: issuecomment-6301319662
  couplings: [compat-migrations]
  open_question: ''
depends_on: [actortemplate-reconciler-takes-again-golden-atelets-trust]
verified: 2026-10-21
---
# A conversation actor a resume left `CRASHED` on atelet's trust-bundle guard returns to the snapshot …

## Change

A conversation actor a resume left `CRASHED` on atelet's trust-bundle guard returns to the snapshot it was resuming from (`outlivedResumeCrash`: a `resume failed: …` crash naming `no trust bundle names given`, once every trust-bundle projection of the actor's template, as the store reads it, names a bundle; `recoverTransientCrash` returns it to `SUSPENDED`, or `PAUSED` with its pause snapshot, its worker torn down and freed; `ResumeActor` then places it on a worker, and the ActorTransitionReconciler recovers one no request does). An actor whose template still names none stays `CRASHED`, so nothing resumes it into the same refusal; the crash text is not added to `transientCrashCauses`, so a golden's crash keeps the template reconciler's own clause and a crash's worker-leaving cause is still read

## Reason

a resume of an actor from a template row rewritten without its trust-bundle name booted with no names under v1.8.3, the guard refused it and the actor stayed `CRASHED` for good, since the crash named no transient cause: the read of the rewritten row as its one name (the migration row above) and the golden heal never reached it ([#332](https://github.com/giantswarm/substrate/issues/332)). Now rolling the release returns such an actor to its last durable snapshot with no operator action. Unit tests on the predicate, on `recoverTransientCrash`, on the reconciler and on `ResumeActor` (red without the change): a `CRASHED` actor carrying the guard's crash and a snapshot, its template row rewritten without the name, resumes `RUNNING` on its snapshot; one whose template still reads with no names stays `CRASHED` with no restore

## History

none: upstream carries the guard's text but neither the read of a rewritten row (it asks for no fallbacks for old state before v1.0.0) nor the transient resume recovery on its `main` (the rows above, upstream pull requests open), so no upstream template names a bundle again after the refusal; falls away with the migration row above

[#335](https://github.com/giantswarm/substrate/pull/335): the fork's own commit, on top of the golden heal's

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: fork-only (class 6): the next re-pin past kagent-dev/kagent 96c40300 is the first whose Substrate carries agent-substrate/substrate#2888; the fork converts state across it instead of cutting over; the row conversion applies this read-migration to every row before re-encoding it, so no legacy row survives, and the commit leaves at that re-pin.
