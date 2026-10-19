---
id: store-migrates-actortemplate-row-release-before-upstream
fork: substrate
theme: compat-migrations
category: compat
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2482]
  exit: as#2482 decision; pre-1.0 policy risk
depends_on: []
commit: giantswarm/substrate#318
verified: 2026-10-19
---
# The store migrates an ActorTemplate row a release before upstream #2048 wrote when it decodes it, …

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2482](https://github.com/agent-substrate/substrate/pull/2482), from the branch `upstream/trust-bundle-legacy-name` (upstream's shape, with its own copies of the unknown-field helpers; both commits). Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#318](https://github.com/giantswarm/substrate/pull/318) and [#333](https://github.com/giantswarm/substrate/pull/333) (the rewritten row): the fork's own commits

## Change

The store migrates an ActorTemplate row a release before upstream #2048 wrote when it decodes it, before unknown fields are dropped, through the `legacyMigration` switch the Actor row's migration above shares: a trust-bundle projection without `names` takes its field 1 (`name`, the one bundle such a release named, which #2048 reserved) as its one name; one with neither, a row from before #2048 that a later release's update rewrote without field 1, takes `egress-mitm.ate.dev` (`legacyTrustBundleName`), the only name atelet allowlisted from the data source's introduction until #2048, while since #2048 a create without a name is refused; a projection that carries `names` is left as it is, and the template's next update persists the rewritten row (`cmd/ateapi/internal/store/atepg/legacy_trust_bundle.go`)

## Reason

upstream #2048 (`715f2daa`, on the line since v1.6.0) replaced `name` with `names` without a migration of the stored rows, so a template persisted before it decoded with no names: every boot from it handed the empty list to atelet, which panicked on it until [#317](https://github.com/giantswarm/substrate/pull/317)'s guard and errors with it since, its golden stayed CRASHED and was re-taken every few seconds, and a new conversation on the agent could not start; kagent writes `names` since its v1.5.0 but reuses a stored template under its revision name and never rewrites it ([#316](https://github.com/giantswarm/substrate/issues/316)). Every update of a template decodes the row without the fields its descriptor does not declare and writes the whole proto back, the reconciler's status checkpoints among them, so on v1.6.0..v1.8.2 the first golden status write dropped field 1: on a production installation all 23 rows from before #2048 held only the path when the read of field 1 arrived, and their golden re-takes failed on the guard ([#330](https://github.com/giantswarm/substrate/issues/330)); a persisted format change ships its migration in the release that changes it. Goldens, snapshots and restores reference the template by uid, so the template row is the only persisted object with the projection. Unit tests decode a template with the legacy name, with `names`, with both and with neither, which takes the allowlisted name; a Postgres test proves the next update persists `names` for both legacy shapes
