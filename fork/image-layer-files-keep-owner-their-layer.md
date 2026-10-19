---
id: image-layer-files-keep-owner-their-layer
fork: substrate
theme: compat-migrations
category: bugfix
status: carried
commits:
  - 'fix(imagecache): keep the owner an image layer gives its files'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1918, agent-substrate/substrate#2036, agent-substrate/substrate#2392]
  exit: as#2392 merged
depends_on: [actors-run-as-their-images-user-workdir]
verified: 2026-10-19
---
# Image-layer files keep the owner their layer tar gives them

## Change

Image-layer files keep the owner their layer tar gives them (atelet records the tar's non-root owners, with the setuid/setgid mode a chown clears, in the layer metadata `whiteouts.json` from its `version` 2; ateom's `FinalizeLayer` applies them to the pooled layer tree with `lchown` under its own `owners-applied` marker, once per layer node-wide, so the merged rootfs presents them with no copy-up; a layer pooled earlier is backfilled from its tar headers when a pull reuses it)

## Reason

atelet unpacks with every capability dropped, so every image-layer file belonged to root: an actor run as its image's `USER` could not write what its image gave it. kagent's Go ADK image declares `USER 65532` and its golden boot failed with `mkdir /config: permission denied`, then `open /config/config.json: permission denied` once the image created `/config` for its user (giantswarm/kagent-upstream#125)

## History

not ours: [agent-substrate/substrate#2392](https://github.com/agent-substrate/substrate/pull/2392) (author aramase, open, `Fixes #2036`, on top of #1918). Falls away at the re-pin onto the first release that carries #2392

[#115](https://github.com/giantswarm/substrate/pull/115)
