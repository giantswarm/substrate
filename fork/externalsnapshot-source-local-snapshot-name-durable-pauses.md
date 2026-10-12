# `ExternalSnapshot.source_local_snapshot_name`, the durable pause's carried field, has number 10001, …

- **Status:** carried
- **Upstream:** none: upstream never had the carried field; when this field goes upstream it takes upstream's next free number and the migration reads 10001 as well
- **Our commit:** [#165](https://github.com/giantswarm/substrate/pull/165) (`cmd/ateapi/internal/store/atepg/legacy_snapshot.go`, `ateapi.proto`)

## Change

`ExternalSnapshot.source_local_snapshot_name`, the durable pause's carried field, has number 10001, far above upstream's range, and the store migrates an Actor row an older release wrote when it decodes it, before unknown fields are dropped: a field 3 whose value is the snapshot URI's own name is the 1.3 copy's source, field 4 (1.4.0 to 1.5.0-rc.1) is the source too, and a snapshot without a template takes 1.3's `ActorStatus` field 11 (`current_actor_template_uid`, the template the guest state was built on); the actor's next update persists the rewritten record

## Reason

field 3 is `source_local_snapshot_name` on 1.3 and upstream's `actor_template_uid` since the line took it, so after a 1.3 upgrade a pause copy named its local snapshot as its template: a resume took the template for replaced, a tag of the actor was refused as taken under another template, the copy was uploaded again; upstream dropped `ActorStatus` field 11 without a migration, so no 1.3 snapshot named its template and none could be tagged ([giantswarm/substrate#153](https://github.com/giantswarm/substrate/issues/153)). Unit tests decode 1.3-, 1.4- and current-encoded actors; a Postgres test proves the next update persists the migration
