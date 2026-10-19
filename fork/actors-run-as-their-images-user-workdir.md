---
id: actors-run-as-their-images-user-workdir
fork: substrate
theme: compat-migrations
category: feature
status: carried
commits:
  - 'imagecache: make the merged rootfs root searchable by non-root'
  - 'test(imagecache): cover a bundle whose rootfs and upper already exist at 0700'
  - 'atelet: make durable-dir volumes writable by non-root containers'
  - 'imagecache: read a file from an image''s layer chain'
  - 'atelet: pass the bundle path into prepareOCIDirectory'
  - 'atelet: honor a container image''s own USER'
  - 'atelet: honor a container image''s WORKDIR'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1906, agent-substrate/substrate#1910, agent-substrate/substrate#1918]
  exit: re-pin onto a base containing as#1906 and as#1918
depends_on: [giantswarm/kagent-upstream:harness-run-claude-code-as-the-images-unprivileged]
verified: 2026-10-19
---
# Actors run as their image's `USER` in its `WORKDIR`

## Change

Actors run as their image's `USER` in its `WORKDIR` (atelet resolves `USER` as Docker does, against the image's own `/etc/passwd` and `/etc/group`; no `USER` is root; the pause container follows the same rule; the process starts in `WORKDIR`, default `/`, created in the actor's upper when the image lacks it), and what a non-root actor needs: the bundle's `rootfs` and overlay `upper` are `0755`, a `durableDir` volume is `0777`, and the kubectl manifest's atelet holds `CAP_DAC_OVERRIDE` to empty the directories a non-root actor left in it (the chart's atelet is privileged already)

## Reason

atelet ran every container as root in `/` whatever its image declared, and on gVisor the container's `/` took the `0700` of the overlay upper, so a process that is not root could exec nothing. kagent's harness images declare `USER 65532` and were run as root regardless; giantswarm/kagent-upstream#125 separates the harness (root, declared as such in the image) from Claude Code and Codex (uid 65532)

## History

not ours: [agent-substrate/substrate#1918](https://github.com/agent-substrate/substrate/pull/1918) (author dims, open on 2026-10-19) and [#1906](https://github.com/agent-substrate/substrate/pull/1906) (merged 2026-10-16, `97de17b1`, in kagent-dev/substrate v0.6.0-alpha3, not in the v0.5.0-alpha2 pin), which #1918 asked to merge first; #1910 is merged. All fall away at the re-pin onto the first release that carries #1918. **The upgrade onto this line breaks every gVisor snapshot**: `runsc restore` checks each container's `Process.User` and `Process.Cwd` against the checkpoint and the pause image sets `USER 65535`, so delete every gVisor actor before the roll and recreate every gVisor template after it (`docs/upgrade.md`, step 4); micro-VM snapshots resume as they were. The harness images must declare their identity (`USER 0:0`, `WORKDIR /`, kagent-upstream#125) before the roll, or the harness itself starts as 65532

`git cherry-pick -x` of the whole stack, in upstream's order: `f55a4de4` (upstream's `8f7eb07d`, rootfs root), with the fork-only test of a restored bundle `f4a98eb1`; `1f43f481` (`895ec3ce`, #1906); `12670632`, `3da2115b`, `136337e7`, `ab5c083e` (`1e0466df`, `fa8578de`, `5925173f`, `d41a2fc2`, #1918). `afb62e18` (agent-substrate/substrate#1910, the manifest's `CAP_DAC_OVERRIDE`) is in the pin. Adapted to the line: `ocispec.Options` keeps its `ActorUID`/`ContainerName` shape and gains only `User` and `Cwd`, paths stay in `ateompath`, and the fork's `TestPrepareOCIDirectory_RegistryRejectionIsTerminal` passes the bundle path; also [#103](https://github.com/giantswarm/substrate/pull/103) (`feat(atelet): carry the image USER and WORKDIR stack of agent-substrate/substrate#1918`), [#101](https://github.com/giantswarm/substrate/pull/101) (`fix(imagecache): carry the rootfs root fix of agent-substrate/substrate#1918`)
