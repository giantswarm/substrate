---
id: suspended-actor-may-be-repointed-at-template
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
commit: giantswarm/substrate#148
verified: 2026-10-19
---
# A suspended actor may be repointed at a template that adds, removes or changes `system_info` …

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch `upstream/repoint-systeminfo-volumes` on the mirror `main`: upstream's check (agent-substrate/substrate `cmd/ateapi/internal/controlapi/actor.go`) predates `system_info` volumes
- **Our commit:** [#148](https://github.com/giantswarm/substrate/pull/148); on `release-1.3` [#149](https://github.com/giantswarm/substrate/pull/149) (1.3.3)

## Change

A suspended actor may be repointed at a template that adds, removes or changes `system_info` volumes and their mounts (`validateTemplateVolumesUnchanged` compares only the other volumes, order included, and each shared container's mounts of them; a durable volume added, removed, renamed, re-sourced or remounted is still refused)

## Reason

kagent's 1.2 line renders every ActorTemplate with an `egress-trust` `system_info` volume; a conversation started on the 1.0 line sits on a template with the `data` volume only, `UpdateActor` refused the move (`volumes differ between the current and the new actor template`), and the conversation stayed on its old template without the egress gateway's CA, every model call failing x509 ([giantswarm/substrate#134](https://github.com/giantswarm/substrate/issues/134), giantswarm/kagent-upstream#170)
