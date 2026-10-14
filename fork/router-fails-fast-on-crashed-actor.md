# atenet-router answers a request to a crashed actor at once

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/router-crashed-actor-no-retry` (`e97cfcdc8`, mirror `main` at the pin `v0.5.0-alpha2` + the change, DCO-signed)
- **Our commit:** the PR adding this file: a `git cherry-pick -x` of the upstream-ready commit

## Change

With request parking on, a `FailedPrecondition` answer of `ResumeActor` makes the router's resumer read the actor
once; an actor in `ACTOR_STATE_CRASHED` ends the flight at once with the refusal, which the HTTP boundary maps to
`503` as before. Any other state, or a failed read, keeps the request parked. `docs/request-parking.md` lists the
case.

## Reason

A crashed actor is refused with `FailedPrecondition` on every resume and leaves that state only through an explicit
`RevertActor` or `DeleteActor`; a crash the control plane recovers by itself it recovers within the `ResumeActor`
call. The router still retried the refusal for its whole 30 s park budget (37 calls in the lab), so the first request
after a sandbox crash waited about 31 s, and kagent's revert and resume of the actor waited with it
([#442](https://github.com/giantswarm/substrate/issues/442)).
