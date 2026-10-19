---
id: actortemplate-reconciler-takes-golden-written-older-snapshot
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(ateapi): exempt the golden actor''s create from the re-take refusal'
  - 'fix(ateapi): take a golden written in an older snapshot layout again'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2474, giantswarm/substrate#151, giantswarm/substrate#284]
  exit: as#2474 decision; expected decline under pre-1.0 policy
depends_on: [giantswarm/substrate#151]
verified: 2026-10-19
---
# The ActorTemplate reconciler takes a golden written in an older snapshot layout again

## Change

The ActorTemplate reconciler takes a golden written in an older snapshot layout again (`GoldenSnapshotStatus.snapshot_layout`, the version `internal/snapshotlayout` numbers — `SingleDurableTar` 1, `PerVolume` 2, `Current` the one the workers write — recorded with the golden tag; a golden recorded in an older layout, or without one by a release before the field, is not done: the reconciler boots the golden actor again on the current workers, takes the snapshot in the current layout and records the new tag, named `<template uid>-<layout>` beside the recorded one, which stays the template's golden until the new one is complete and is then deleted with its snapshot; a create on the template answers `Unavailable` until then, the golden actor's own create excepted, which boots fresh whatever the template's golden, its snapshot being the golden; a re-take that fails keeps the failure reasons and bounds and the old golden beside the failure; a golden in the current layout is left alone; the template delete removes every golden tag; `cmd/ateom-gvisor/durable.go` names the layout it writes)

## Reason

the reconciler took each golden once and skipped a template with a golden tag for good, and kagent writes a new ActorTemplate only when the agent spec changes, so after a release that changed the snapshot layout (the row of agent-substrate/substrate#2110 above) every new actor created from a golden written before it restored a snapshot its worker could not read — refused since the refusal's row, converted on every restore since the conversion's — and every new conversation on every existing agent failed until the goldens were reset by hand with `docs/snapshot-reset.md` ([#272](https://github.com/giantswarm/substrate/issues/272)). Now the goldens follow the release on their own

## History

[agent-substrate/substrate#2474](https://github.com/agent-substrate/substrate/pull/2474) (ours, open; from branch `upstream/golden-layout-retake`, the port of both commits onto upstream `main` `29c049fc` as one commit on Actor Lifecycle v2, without the golden-tag migration of [#151](https://github.com/giantswarm/substrate/pull/151), plus one docs commit for the API guide and glossary; [#284](https://github.com/giantswarm/substrate/issues/284)). Upstream's `AGENTS.md` asks for no fallbacks for old formats before v1.0.0, so upstream may decline it; then it is carried until a release after 1.0 makes it moot

[#282](https://github.com/giantswarm/substrate/pull/282): the fork's own commit, and [#288](https://github.com/giantswarm/substrate/pull/288): the golden actor's own create exempt from the re-take's refusal and taking no source from the recorded golden, without which the re-take refused itself on every pass and no golden was taken again
