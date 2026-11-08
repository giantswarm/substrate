---
id: actortemplate-reconciler-sweeps-goldens-of-deleted-templates
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateapi): sweep the goldens a failed template delete left behind'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR, together with the template delete that takes the row first
depends_on: [deleteactortemplate-deletes-template-row-first-under-callers]
verified: 2026-11-08
---
# The ActorTemplate reconciler sweeps the goldens of deleted templates

## Change

Each template resync also sweeps the reserved atespace `ate-golden` under the lease `lease:actortemplate-golden-sweep`, so one ate-api replica sweeps at a time: a golden actor (named after a template uid, created from a template) or golden tag (taken from such an actor, named after its uid or `<uid>-<layout>`) whose template uid has no row and that is older than `--golden-sweep-grace-period` (10 minutes by default) is deleted, the actor in any state, the tag with its snapshot, and one line per template uid names what was removed. The goldens are listed before the template rows, so a golden of a template created meanwhile is always matched by its row; a golden whose row exists, one younger than the grace period, and an object in `ate-golden` the golden flow did not make are never touched. A delete that fails is logged and retried by the next sweep. Unit tests with the fake store (removal, the row and age guards, retry, a failed listing), a lease test against PostgreSQL, and a functional test against a running ate-api whose template delete fails on unreachable object storage after the row.

## Reason

`DeleteActorTemplate` deletes the template row first and the golden actor and tag after it, so a cleanup that fails after the row (object storage unreachable, a crash of ate-api between the steps) leaves the golden in `ate-golden` with nothing to rediscover it by: a retry of the delete answers `NotFound`, and the golden and its snapshot stayed until someone deleted them by hand ([#177](https://github.com/giantswarm/substrate/issues/177)).

## History

`giantswarm`: this file's pull request; `release-1.3`: its backport.
