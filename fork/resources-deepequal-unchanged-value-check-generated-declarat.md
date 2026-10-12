# `resources.DeepEqual`, the unchanged-value check of the generated declarative validation, compares …

- **Status:** carried
- **Upstream:** none: upstream `main` has the same `DeepEqual`; to file, queued in the upstream engagement list
- **Our commit:** [#234](https://github.com/giantswarm/substrate/pull/234) (`fix(resources): compare repeated message fields with proto.Equal`)

## Change

`resources.DeepEqual`, the unchanged-value check of the generated declarative validation, compares a slice of proto messages (a repeated message field) element by element with `proto.Equal`, a nil and an empty one being equal

## Reason

it handed the slice to `reflect.DeepEqual`, which also compares each message's internal state; the RPC logger's marshal fills the size cache, so a repeated message field and its clone differed and an `+k8s:immutable` repeated field (`Actor.existing_volumes`, row below) failed every `CreateActor` with "field is immutable"
