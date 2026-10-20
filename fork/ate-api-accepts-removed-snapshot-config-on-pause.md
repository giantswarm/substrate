---
id: ate-api-accepts-removed-snapshot-config-on-pause
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): accept the removed SnapshotConfig.on_pause for a transition'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2309, agent-substrate/substrate#2542]
  exit: 'dropped at the conversion re-pin, where field 1 becomes storage_location; every client regenerates its protos before it'
  class: 1
  destination: ''
  first_step: 'regenerate the lab client protos away from v1.8.1 before the re-pin, then drop (giantswarm/agentlab)'
  source: 'giantswarm @ ee2d73dc8; upstream/onpause-tolerance @ bf475a04c (agent-substrate/substrate#2542)'
  test: 'the lab workspaces-actors and substrate-tunnel proofs on regenerated protos'
  recheck: 'the conversion re-pin'
  owner: unassigned
  draft: ''
  decision: issuecomment-6301319662
  couplings: [compat-migrations]
  open_question: ''
depends_on: []
verified: 2026-10-21
---
# ate-api accepts the removed SnapshotConfig.on_pause for a transition

## Change

The new package `internal/removedfields` lists the fields the API removed that a client may still send: `ateapi.SnapshotConfig` field 1 with the varint wire type, the `on_pause` a client built against the 1.8 protos sends. `RejectUnknownFieldsUnaryInterceptor` (`internal/ateinterceptors/rejectunknown.go`) lets a listed field pass and leaves it in place. It logs each one once per principal (`Request carries a removed field; ignoring it`, with `field`, `method` and `principal`) and refuses every other unknown field as before, including field 1 with another wire type, which upstream `main` uses for the string `storage_location`. `unmarshalStored` (`cmd/ateapi/internal/store/atepg/atepg.go`) keeps a listed field when it discards a row's unknown fields (`removedfields.DiscardUnknown`, which replaces the line's `discardUnknown`), so a template is returned with `on_pause` as it was written. A template a 1.8 release stored carries it in the same encoding and is returned with it too. Nothing reads the value, and the proto and generated code are unchanged. Unit tests cover the interceptor and the package, a store test covers the read, and a functional test sends the field over gRPC and reads it back.

## Reason

kagent 1.10.x sets `on_pause` on every ActorTemplate it writes and compares the template it reads back with its compiled one by `proto.Equal`. Without this change, every new agent, agent edit and template recompile of such a kagent is refused on 1.9.x with `InvalidArgument` (`ActorTemplateRejected`). A shim that drops the field instead leaves those agents in `ActorTemplateConflict`, and a kagent restart puts the agents created before the step there as well. The change leaves once every consumer's kagent is built against protos without the field (kagent-dev/kagent dropped it with its Substrate v0.5.0-alpha bump) and ate-api logs no removed field.

## History

[agent-substrate/substrate#2542](https://github.com/agent-substrate/substrate/pull/2542), open, from the branch `upstream/onpause-tolerance` on upstream `main` @ `f03327bd`; a compatibility shim for the field [#2309](https://github.com/agent-substrate/substrate/pull/2309) removed, which upstream's pre-v1.0 policy may decline

[#372](https://github.com/giantswarm/substrate/pull/372): the fork's cherry-pick of the upstream pull request's two commits as one; `unmarshalStored` differs from upstream's, since the line migrates legacy rows there

2026-10-20: agent-substrate/substrate#2309 (merged 2026-10-07, `44a201b3`, in the pin) is the removal this shim answers, not its exit, so the outcome stays open on #2542.

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: drop (class 1): no installation runs a kagent built with on_pause, field 1 is storage_location after the re-pin, and the lab client regenerates its protos before it.
