---
id: resume-takes-lost-golden-again
fork: substrate
theme: golden-boot
category: bugfix
status: carried
commits:
  - 'fix(ateapi): take a lost golden again on the first resume'
upstream:
  outcome: to-file
  refs: [giantswarm/substrate#170, giantswarm/substrate#561]
  exit: 'an upstream change that recovers the clones of a deleted golden tag; the line drops its commit at the re-pin onto a release carrying it'
depends_on: [actortemplate-reconciler-takes-golden-written-older-snapshot]
verified: 2026-11-08
---
# A resume takes a lost golden again

## Change

A resume of an actor that still borrows its template's golden (created from the golden default, never suspended since, its snapshot under a prefix of `ate-golden` rather than its own) first checks that the borrowed snapshot's objects are still in object storage (`objectstore.HasObjects`, in the step `EnsureGoldenSource` before the worker is assigned). When they are gone, the actor is moved onto its template's current golden tag when that one is complete and its objects are present, as a create from the template would start it now. When the template has no restorable golden, the recorded golden tag is deleted and the record cleared, which sends the template back to the ActorTemplate reconciler to boot a new golden actor on the current workers; the resume waits for the new golden within a budget of its own (two minutes, polled every two seconds) and answers `Unavailable` once it is spent, so a retry outlives it. A golden the take fails on for good (a terminal `GoldenSnapshotStatus.error_message`) refuses the resume with `FailedPrecondition`, reason `GOLDEN_SNAPSHOT_UNAVAILABLE`, `resumable: "false"`. An actor that owns its snapshot never reads the golden and is left alone. Unit tests against PostgreSQL and the in-memory object store, and functional tests through the gRPC service and the fake atelet, cover the move, the new take, the spent budget and the refusal; the functional tests' golden fixture writes the golden snapshot's objects, as the golden flow's copy does.

## Reason

Since the v0.4.0-alpha1 pin no resume reads the template's golden: an actor restores its own snapshot, and only an actor created from the golden and never suspended still borrows the golden tag's snapshot. A golden tag deleted while such actors exist, by hand or by the reconciler replacing a golden taken in an older snapshot layout (its ledger file), collects that snapshot, so their resume restored a snapshot with no objects and crashed them, although they held nothing beyond the golden, and every later resume was refused as `ACTOR_CRASHED`. A template whose recorded golden tag was gone also kept that record for good, so every create on it failed.

## History

`giantswarm`: [#561](https://github.com/giantswarm/substrate/pull/561).

to file — prepared in the fork: branch [`upstream/golden-resnapshot`](https://github.com/giantswarm/substrate/tree/upstream/golden-resnapshot) (`88bdd021` on the mirror `main` @ `c2edd34d`, DCO-signed, no fork references, its message the pull-request text). Upstream keeps object storage behind a plugin with no list call, places the golden in the template's atespace and names it after the template, and has no golden re-take that replaces a tag, so the port detects the lost golden by the source tag's row and snapshot UUID instead of by its objects, moves the actor onto the current golden as a new snapshot generation, and refuses a failed golden with a plain `FailedPrecondition`; its unit tests and the `controlapi` and functional suites pass on the mirror.
