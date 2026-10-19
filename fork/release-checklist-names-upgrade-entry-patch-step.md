---
id: release-checklist-names-upgrade-entry-patch-step
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: none
depends_on: []
commit: giantswarm/substrate#378
verified: 2026-10-19
---
# release checklist names the upgrade entry of a patch release

- **Status:** carried
- **Upstream:** not ours: the step names `docs/upgrade.md`, a file of this line
- **Our commit:** [#378](https://github.com/giantswarm/substrate/pull/378)

## Change

`docs/dev/release-notes.md`, "Cut a release", gains the step "add the `docs/upgrade.md` entry for this patch" before the tag is pushed, and the later steps are renumbered.

## Reason

`docs/upgrade.md` carries one entry per released patch line (what changes in persisted data, whether a migration runs, whether a reset is needed); without a release step that names it, a patch ships without its entry and the statement that it needs no reset rests on reading the diff.
