---
id: ate-api-adopts-openfga-schema-that-openfgas
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(atepg): adopt an OpenFGA schema created by OpenFGA''s migrations'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1839]
  exit: line release after which no 1.3 installs remain
depends_on: []
verified: 2026-10-19
---
# ate-api adopts an OpenFGA schema that OpenFGA's own embedded migrations created

## Change

ate-api adopts an OpenFGA schema that OpenFGA's own embedded migrations created (`adoptOpenFGASchema`, before Goose runs and under the migration lock: a database at Substrate migration 1 whose OpenFGA ledger `goose_db_version` is at the pinned OpenFGA migration 6 gets migration 2, `000002_openfga.sql`, recorded as applied, the schema it creates being exactly OpenFGA's migration 6; another OpenFGA version is refused by name; OpenFGA's ledger stays, so a 1.3.x ate-api still finds its migrations applied)

## Reason

1.3.x created the OpenFGA tables through OpenFGA's own migrations; the 1.4 line creates them in Substrate's migration 2, which failed on the existing tables (`partial migration error (type:sql,version:2): ERROR: relation "tuple" already exists`), and the new ate-api-server crash-looped on every 1.3.1 → 1.4.x upgrade while the old replicas kept serving ([giantswarm/substrate#137](https://github.com/giantswarm/substrate/issues/137))

## History

to file — prepared in the fork: branch `upstream/openfga-schema-adopt` on the mirror `main`: agent-substrate/substrate#1839 introduced migration 2 without adopting an existing schema, which breaks every upgrade from a release before it

[#138](https://github.com/giantswarm/substrate/pull/138)
