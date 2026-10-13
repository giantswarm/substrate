# ate-api accepts the removed SnapshotConfig.on_pause for a transition

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2542](https://github.com/agent-substrate/substrate/pull/2542), open, from the branch `upstream/onpause-tolerance` on upstream `main` @ `f03327bd`; a compatibility shim for the field [#2309](https://github.com/agent-substrate/substrate/pull/2309) removed, which upstream's pre-v1.0 policy may decline
- **Our commit:** [#371](https://github.com/giantswarm/substrate/pull/371): the fork's cherry-pick of the upstream commit, unchanged

## Change

`RejectUnknownFieldsUnaryInterceptor` (`internal/ateinterceptors/rejectunknown.go`) drops the fields of its `removedFields` table from a request's unknown fields before the unknown-field check: `ateapi.SnapshotConfig` field 1 with the varint wire type, the `on_pause` a client built against the 1.8 protos sends. The value is ignored. The interceptor logs each dropped field once per principal (`Request carries a removed field; ignoring it`, with `field`, `method` and `principal`). Every other unknown field is refused as before, including field 1 with another wire type, which upstream `main` uses for the string `storage_location`. The proto and generated code are unchanged. Unit tests cover the dropped field, another unknown tag, field 1 with another wire type or on another message, and a request without it. A functional test sends the field over gRPC.

## Reason

kagent 1.10.x sets `on_pause` on every ActorTemplate it writes. Without this change, every new agent, agent edit and template recompile of such a kagent is refused on 1.9.x with `InvalidArgument` (`ActorTemplateRejected`). The change leaves once every consumer's kagent is built against protos without the field (kagent-dev/kagent dropped it with its Substrate v0.5.0-alpha bump) and ate-api logs no dropped field.
