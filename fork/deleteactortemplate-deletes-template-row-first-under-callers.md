# `DeleteActorTemplate` deletes the template row first, under the caller's preconditions checked in …

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch [`upstream/template-delete-row-first`](https://github.com/giantswarm/substrate/tree/upstream/template-delete-row-first) (`064172d0`, the patch `fcce668c` and a lint fix on the mirror `main` @ `c2e2e130`, DCO-signed, no fork references, its message the pull-request text)
- **Our commit:** `giantswarm`: [#168](https://github.com/giantswarm/substrate/pull/168); `release-1.3`: [#169](https://github.com/giantswarm/substrate/pull/169) (that line has no delete preconditions)

## Change

`DeleteActorTemplate` deletes the template row first, under the caller's preconditions checked in the delete statement, then the golden actor and the golden tag; a refused or interrupted delete leaves the template with its golden intact, and a golden the cleanup fails to remove stays in `ate-golden` under the template's uid, named in the error, a retry answering `NotFound` and the leftover deleted on its own (unit tests for a delete the store refuses and a cleanup that fails after the row; `TestGoldenTagLifecycle` proves against a running ate-api that a delete refused on a stale version leaves the golden tag and an actor of the template resuming)

## Reason

the golden actor and tag went first, so a row delete the store refused (a uid or version conflict, a store error) or a crash of ate-api between the steps left a template whose golden tag was gone: every actor of it was refused on resume as not resumable, `GOLDEN_SNAPSHOT_UNAVAILABLE`, for as long as the template existed ([giantswarm/substrate#105](https://github.com/giantswarm/substrate/issues/105)). Upstream's order kept the row for a retry to rediscover the golden by; a row that outlives its golden is the worse leak
