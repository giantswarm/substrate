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
  exit: 'dropped at the conversion re-pin; its stop-version (Substrate 1.16.0) is past every 1.3 installation'
  class: 1
  destination: ''
  first_step: 'drop at the conversion re-pin (giantswarm/substrate)'
  source: 'giantswarm @ 6c75b93cb; upstream/openfga-schema-adopt @ 1401113dd unused'
  test: 'the conversion lab upgrade from Substrate 1.16.3'
  recheck: 'the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: 'issuecomment-6301319662, amended issuecomment-6339634309'
  couplings: [compat-migrations]
  open_question: ''
depends_on: []
verified: 2026-10-21
---
# ate-api adopts an OpenFGA schema that OpenFGA's own embedded migrations created

## Change

ate-api adopts an OpenFGA schema that OpenFGA's own embedded migrations created (`adoptOpenFGASchema`, before Goose runs and under the migration lock: a database at Substrate migration 1 whose OpenFGA ledger `goose_db_version` is at the pinned OpenFGA migration 6 gets migration 2, `000002_openfga.sql`, recorded as applied, the schema it creates being exactly OpenFGA's migration 6; another OpenFGA version is refused by name; OpenFGA's ledger stays, so a 1.3.x ate-api still finds its migrations applied)

## Reason

1.3.x created the OpenFGA tables through OpenFGA's own migrations; the 1.4 line creates them in Substrate's migration 2, which failed on the existing tables (`partial migration error (type:sql,version:2): ERROR: relation "tuple" already exists`), and the new ate-api-server crash-looped on every 1.3.1 → 1.4.x upgrade while the old replicas kept serving ([giantswarm/substrate#137](https://github.com/giantswarm/substrate/issues/137))

## History

to file — prepared in the fork: branch `upstream/openfga-schema-adopt` on the mirror `main`: agent-substrate/substrate#1839 introduced migration 2 without adopting an existing schema, which breaks every upgrade from a release before it

[#138](https://github.com/giantswarm/substrate/pull/138)

2026-10-20: agent-substrate/substrate#1839 (merged 2026-09-24, `5fb129d8`, in the pin) introduced the migration this patch adopts around, not its exit, so the outcome stays to-file.

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: drop (class 1): the conversion's stop-version, Substrate 1.16.0, is past the 1.3 to 1.4 step this adopts across.

2026-10-22: amended in the fork-reduction review of the compat-migrations theme: the conversion re-pin is the first past Substrate GA (fallback 2026-11-24), with one conversion written then against the frozen format; whether it runs as a one-shot agent-platform Job or as fork code for that one re-pin is decided then.
