---
id: conversation-actor-resume-left-crashed-atelets-trust
fork: substrate
theme: compat-migrations
category: compat
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: line release after which no such actors remain
depends_on: [actortemplate-reconciler-takes-again-golden-atelets-trust]
commit: giantswarm/substrate#335
verified: 2026-10-19
---
# A conversation actor a resume left `CRASHED` on atelet's trust-bundle guard returns to the snapshot …

- **Status:** carried
- **Upstream:** none: upstream carries the guard's text but neither the read of a rewritten row (it asks for no fallbacks for old state before v1.0.0) nor the transient resume recovery on its `main` (the rows above, upstream pull requests open), so no upstream template names a bundle again after the refusal; falls away with the migration row above
- **Our commit:** [#335](https://github.com/giantswarm/substrate/pull/335): the fork's own commit, on top of the golden heal's

## Change

A conversation actor a resume left `CRASHED` on atelet's trust-bundle guard returns to the snapshot it was resuming from (`outlivedResumeCrash`: a `resume failed: …` crash naming `no trust bundle names given`, once every trust-bundle projection of the actor's template, as the store reads it, names a bundle; `recoverTransientCrash` returns it to `SUSPENDED`, or `PAUSED` with its pause snapshot, its worker torn down and freed; `ResumeActor` then places it on a worker, and the ActorTransitionReconciler recovers one no request does). An actor whose template still names none stays `CRASHED`, so nothing resumes it into the same refusal; the crash text is not added to `transientCrashCauses`, so a golden's crash keeps the template reconciler's own clause and a crash's worker-leaving cause is still read

## Reason

a resume of an actor from a template row rewritten without its trust-bundle name booted with no names under v1.8.3, the guard refused it and the actor stayed `CRASHED` for good, since the crash named no transient cause: the read of the rewritten row as its one name (the migration row above) and the golden heal never reached it ([#332](https://github.com/giantswarm/substrate/issues/332)). Now rolling the release returns such an actor to its last durable snapshot with no operator action. Unit tests on the predicate, on `recoverTransientCrash`, on the reconciler and on `ResumeActor` (red without the change): a `CRASHED` actor carrying the guard's crash and a snapshot, its template row rewritten without the name, resumes `RUNNING` on its snapshot; one whose template still reads with no names stays `CRASHED` with no restore
