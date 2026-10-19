---
id: lines-own-postgresql-migrations-live-cmd-ateapi
fork: substrate
theme: durable-pause
category: gs-specific
status: carried
commits:
  - 'fix(atepg): keep the line''s own migrations in a ledger of their own'
upstream:
  outcome: fork-only
  refs: []
  exit: drops when no fork schema remains
depends_on: [durable-pauses-background-commit-holds-no-lease]
verified: 2026-10-19
---
# The line's own PostgreSQL migrations live in `cmd/ateapi/internal/store/atepg/migrations_fork/` …

## Change

The line's own PostgreSQL migrations live in `cmd/ateapi/internal/store/atepg/migrations_fork/` with their own Goose ledger, `fork_schema_migrations`, applied after upstream's `migrations/` and its ledger `schema_migrations`, which carry upstream's files unchanged. A 1.5.x database, which applied the lease holder as version 3 of `schema_migrations`, is moved once at startup under the migration lock: the fork ledger is created with its version 1 applied and version 3 leaves `schema_migrations`, so upstream's `000003_access_policies.sql` applies as 3; `leases.holder` without the fork ledger in any other state is refused before any migration runs. `hack/verify/postgresql-migrations.sh` checks both directories. Tests: a 1.5 database (its lease row kept, the access-policy tables written, a restart moving nothing), a fresh one, and the refused state

## Reason

renumbering upstream's migrations around the line's shifts every later upstream migration on every re-pin and leaves upstream files whose number differs on the line; with two ledgers a re-pin carries `migrations/` as upstream ships it. A 1.6 ateapi rolled back to 1.5.x finds `schema_migrations` at 3 and the lease columns it reads

## History

none: fork-only, upstream carries no migrations but its own

`giantswarm`: the 2026-10-08 re-pin (`sync/20261008-v0.4.0-alpha1`)
