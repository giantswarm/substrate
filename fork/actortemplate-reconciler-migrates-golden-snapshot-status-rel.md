---
id: actortemplate-reconciler-migrates-golden-snapshot-status-rel
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): migrate golden statuses recorded before golden tags'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2105]
  exit: 'folded at the conversion re-pin into the row conversion, which applies it before re-encoding; drops with that conversion at the following re-pin'
  class: 6
  destination: 'giantswarm/substrate ate-api row conversion of the conversion re-pin'
  first_step: 'at the conversion re-pin, the row conversion applies this migration before re-encoding (giantswarm/substrate)'
  source: 'giantswarm @ 8e522e939; conversion: new branch'
  test: 'the conversion lab upgrade from Substrate 1.16.3; the conversion counter reports no failed row'
  recheck: 'Substrate GA (agent-substrate/substrate#2004), fallback 2026-11-24, with the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: 'issuecomment-6301319662, amended issuecomment-6339634309'
  couplings: [compat-migrations]
  open_question: ''
depends_on: [giantswarm/agentgateway-upstream:a-resume-refusal-marked-not-resumable-is-answered-at-once]
verified: 2026-10-21
---
# The ActorTemplate reconciler migrates a golden snapshot status a release before golden tags …

## Change

The ActorTemplate reconciler migrates a golden snapshot status a release before golden tags recorded into the current shape (`GoldenSnapshotStatus` field 1 was an `ExternalSnapshot` and is the `ObjectRef golden_tag` since 1.1.0, so a 1.0.x status reads as a golden tag named after its snapshot URI with no name): the golden actor that release kept suspended gets the template its snapshot was built under recorded, which that release did not record and the tag workflow requires, its snapshot is copied into the golden tag through `CreateTag` and the actor released, as the golden flow does; a golden whose actor and tag are gone, whose actor holds no snapshot or whose copy is refused fails the template `GoldenSnapshotLost` naming what is missing, so a reader tells a lost golden from an unmigrated one, and the migration runs once per template, its outcome being terminal. Until a template is migrated, a Golden data resume and a create on it answer `Unavailable` (a retry outlives it), never the not-resumable `GOLDEN_SNAPSHOT_UNAVAILABLE`; a resume on a failed golden carries the failure, the lost reason included. `docs/upgrade.md` step 6 carries the note

## Reason

1.1.0 changed the field's meaning without a migration, so after the upgrade every template created on 1.0.x read as one whose golden tag has no name and every actor created from it was refused as not resumable (`GOLDEN_SNAPSHOT_UNAVAILABLE`, `resumable: "false"`) although its golden actor still held the snapshot, and nothing told a lost golden from an unmigrated one ([giantswarm/substrate#139](https://github.com/giantswarm/substrate/issues/139)). The actors' turns are kagent's concern: a 1.0-era template lacks the egress trust configuration, so kagent moves such sessions to their agent's current revision ([giantswarm/kagent-upstream#170](https://github.com/giantswarm/kagent-upstream/issues/170))

## History

to file — prepared in the fork: branch `upstream/golden-status-migration` on the mirror `main`. At the 2026-10-08 re-pin onto v0.4.0-alpha1: upstream removed `onResume.fromData` and the `DATA_ON_GOLDEN` restore scope (agent-substrate/substrate#2105, `6a35150e`), so no resume reads the template's golden tag any more; the reconciler migration and the `Unavailable` refusal of `CreateActor` stay, the resume hunks are gone, and `golden_snapshot.go` holds only `legacyGoldenSnapshot` and `goldenSnapshotAwaitingMigration`.

`78d4ad00` ([#151](https://github.com/giantswarm/substrate/pull/151), first released in 1.5.0, after 1.4.1-rc.7); `release-1.3`: `9024e3eb` ([#152](https://github.com/giantswarm/substrate/pull/152), released in 1.3.4-rc.1)

2026-10-20: agent-substrate/substrate#2105 (merged 2026-10-02, `6a35150e`, in the pin) reshaped the patch but is not its exit, so the outcome stays to-file.

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: fork-only (class 6): the next re-pin past kagent-dev/kagent 96c40300 is the first whose Substrate carries agent-substrate/substrate#2888; the fork converts state across it instead of cutting over; the row conversion applies this read-migration to every row before re-encoding it, so no legacy row survives, and the commit leaves at that re-pin.

2026-10-22: amended in the fork-reduction review of the compat-migrations theme: the conversion re-pin is the first past Substrate GA (fallback 2026-11-24), with one conversion written then against the frozen format; whether it runs as a one-shot agent-platform Job or as fork code for that one re-pin is decided then.
