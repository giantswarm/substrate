---
id: release-checklist-requires-upgrade-entry-of-minor
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'docs(release): the release checklist requires the upgrade entry of a minor, before its first release candidate'
upstream:
  outcome: fork-only
  refs: []
  exit: none
depends_on: []
verified: 2026-10-19
---
# release checklist requires the upgrade entry of a minor, before its first release candidate

## Change

`docs/dev/release-notes.md`, "Cut a release", step 1 requires the `docs/upgrade.md` entry for every release that changes the line's version step, a minor (`vX.Y.0`, the step from the previous minor) as well as a patch, and makes the minor's entry a precondition of cutting its first release candidate (`vX.Y.0-rc.1`).

## Reason

The step named the entry for patch releases only, so a minor shipped without one: v1.10.0-rc.5 had no 1.9 to 1.10 entry until it was added by hand. A candidate that ships without the entry gives a tester no statement on persisted data, migration or reset for the step.

## History

not ours: the step names `docs/upgrade.md`, a file of this line

[#459](https://github.com/giantswarm/substrate/pull/459)
