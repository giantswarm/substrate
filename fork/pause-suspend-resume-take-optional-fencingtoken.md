# Pause, Suspend and Resume take an optional `FencingToken`

- **Status:** carried
- **Upstream:** none upstream; to file: written upstream-ready; upstream would number the fields in its own range
- **Our commit:** `feat(ateapi): fence Pause, Suspend and Resume with a lease token` (`giantswarm`), its backport on `release-1.3`; also [#182](https://github.com/giantswarm/substrate/pull/182) (`feat(ateapi): fence Pause, Suspend and Resume with a lease token`)

## Change

Pause, Suspend and Resume take an optional `FencingToken` (holder and lease generation, field 10001 of each request); ate-api records the newest token per Actor in `ActorStatus.fencing_token` (10001) under the Actor's lease and rejects an older one with `FailedPrecondition`; a Resume of a running Actor with a newer token records it without touching the runtime; a request without a token is admitted as before (`cmd/ateapi/internal/controlapi/fencing.go`, unit tests in `fencing_test.go`)

## Reason

kagent's quiescence claim holds a 30 s lease; a holder frozen past it while its Pause or Suspend is in flight is taken over, the new holder settles the boundary on a RUNNING Actor and releases, and the late request then pauses an Actor the session believes is running. The token lets the new holder fence out the old one on the runtime ([giantswarm/kagent-upstream#181](https://github.com/giantswarm/kagent-upstream/issues/181); kagent passes the token in its second step)
