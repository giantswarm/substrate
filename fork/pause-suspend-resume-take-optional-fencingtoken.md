---
id: pause-suspend-resume-take-optional-fencingtoken
fork: substrate
theme: durable-pause
category: feature
status: carried
commits:
  - 'feat(ateapi): fence Pause, Suspend and Resume with a lease token'
upstream:
  outcome: to-file
  refs: []
  exit: "an agent-substrate/substrate design issue settles the field numbers; the line moves to them with a conversion"
depends_on: [giantswarm/kagent-upstream:session-fence-a-frozen-quiescence-holders-late-pause]
verified: 2026-11-27
---
# Pause, Suspend and Resume take an optional `FencingToken`

## Change

Pause, Suspend and Resume take an optional `FencingToken` (holder and lease generation, field 10001 of each request); ate-api records the newest token per Actor in `ActorStatus.fencing_token` (10001) under the Actor's lease and rejects an older one with `FailedPrecondition`; a Resume of a running Actor with a newer token records it without touching the runtime; a request without a token is admitted as before (`cmd/ateapi/internal/controlapi/fencing.go`, unit tests in `fencing_test.go`)

## Reason

kagent's quiescence claim holds a 30 s lease; a holder frozen past it while its Pause or Suspend is in flight is taken over, the new holder settles the boundary on a RUNNING Actor and releases, and the late request then pauses an Actor the session believes is running. The token lets the new holder fence out the old one on the runtime ([giantswarm/kagent-upstream#181](https://github.com/giantswarm/kagent-upstream/issues/181); kagent passes the token in its second step)

## History

none upstream; to file: written upstream-ready; upstream would number the fields in its own range

`feat(ateapi): fence Pause, Suspend and Resume with a lease token` (`giantswarm`), its backport on `release-1.3`; also [#182](https://github.com/giantswarm/substrate/pull/182) (`feat(ateapi): fence Pause, Suspend and Resume with a lease token`)

2026-11-27: replayed onto kagent-dev/substrate v1.0.1 (`release-1.0@7ce6f439`): `FencingToken{holder 1, generation 2}`; `SuspendActorRequest.fencing_token`, `ResumeActorRequest.fencing_token` and `ActorStatus.fencing_token` at 10001. v1.0.1 has no PauseActor, so only Suspend and Resume are fenced..
