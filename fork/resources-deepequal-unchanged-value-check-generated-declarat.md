---
id: resources-deepequal-unchanged-value-check-generated-declarat
fork: substrate
theme: workspace-volumes
category: bugfix
status: carried
commits:
  - 'fix(resources): compare repeated message fields with proto.Equal'
upstream:
  outcome: to-file
  refs: [giantswarm/substrate#234]
  exit: upstream PR
depends_on: [existing-volume-mounted-per-actor-at-sub]
verified: 2026-10-20
---
# `resources.DeepEqual`, the unchanged-value check of the generated declarative validation, compares …

## Change

`resources.DeepEqual`, the unchanged-value check of the generated declarative validation, compares a slice of proto messages (a repeated message field) element by element with `proto.Equal`, a nil and an empty one being equal

## Reason

it handed the slice to `reflect.DeepEqual`, which also compares each message's internal state; the RPC logger's marshal fills the size cache, so a repeated message field and its clone differed and an `+k8s:immutable` repeated field (`Actor.existing_volumes`, row below) failed every `CreateActor` with "field is immutable"

## History

to file: upstream `main` has the same `DeepEqual`

[#234](https://github.com/giantswarm/substrate/pull/234) (`fix(resources): compare repeated message fields with proto.Equal`)

2026-10-20: agent-substrate/substrate#231 (merged 2026-06-12, a context fix in `memorypullcache`) was a wrong ref and is removed; no upstream item proposes the `proto.Equal` comparison (agent-substrate/substrate and kagent-dev/substrate searched 2026-10-20, upstream `main` keeps the same `DeepEqual`), and [#234](https://github.com/giantswarm/substrate/pull/234) (merged 2026-10-09) is the line's own pull request that carries this fix, so the outcome stays to-file.
