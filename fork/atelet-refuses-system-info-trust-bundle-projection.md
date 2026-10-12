# atelet refuses a system-info trust-bundle projection with no names

- **Status:** carried
- **Upstream:** agent-substrate/substrate `601c03ba` ("SystemInfo: Implement system-roots.ate.dev trust bundle", merged 2026-10-06, after the pin; `trustbundle.Source.Combined` refuses an empty list). Falls away at the re-pin onto a tag that contains it
- **Our commit:** [#317](https://github.com/giantswarm/substrate/pull/317): the minimal equivalent of upstream `601c03ba`, whose system-roots bundle and API change the line does not carry

## Change

atelet refuses a system-info trust-bundle projection with no names (`collectData` answers `no trust bundle names given` instead of indexing `names[0]`; `projectsBundle` matches with `slices.Contains`)

## Reason

a projection whose `names` list was empty panicked the atelet (`index out of range [0] with length 0`, exit 2) inside `AteomHerder.Run`, so every actor on its node went down and the boot crash-looped from atelet to atelet ([#314](https://github.com/giantswarm/substrate/issues/314)); now the boot fails with the error
