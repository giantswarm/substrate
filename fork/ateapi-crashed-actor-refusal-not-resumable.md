# A crashed actor's resume refusal is marked not resumable

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/ateapi-crashed-actor-not-resumable` (`4f3cb20d1`, mirror `main` at the pin `v0.5.0-alpha2` + the change, DCO-signed)
- **Our commit:** [#444](https://github.com/giantswarm/substrate/pull/444): a `git cherry-pick -x` of the upstream-ready commit

## Change

`ResumeActor`'s refusal of an actor in `ACTOR_STATE_CRASHED` (`FailedPrecondition`, message unchanged) carries an
AIP-193 `ErrorInfo` in the `substrate.dev` domain, reason `ACTOR_CRASHED`, metadata `resumable: "false"`.
`apierror.NotResumable` builds it, `apierror.FromError` keeps it on the wire, `apierror.IsNotResumable` reads it.
atenet's Go resumer answers a refusal so marked at once while parking; `docs/request-parking.md` lists the case.

## Reason

atenet-router parks `FailedPrecondition` and retried a crashed actor's refusal for its whole 30 s park budget (37
calls in the lab), although a crashed actor leaves that state only through an explicit `RevertActor` or
`DeleteActor` ([#442](https://github.com/giantswarm/substrate/issues/442)). The line's router dataplane, the
agentgateway line from 2.3.0 on, already answers a refusal carrying this directive at once (its ledger file
`a-resume-refusal-marked-not-resumable-is-answered-at-once.md`); this change is its producer for a crashed actor, and
the Go resumer honors the same directive. kagent's gateway reads the refusal as a send its runtime did not accept and
reverts and resumes the actor.
