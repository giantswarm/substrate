---
id: image-layer-files-keep-owner-their-layer
fork: substrate
theme: golden-boot
category: bugfix
status: carried
commits:
  - 'fix(imagecache): keep the owner an image layer gives its files'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1918, agent-substrate/substrate#2036, agent-substrate/substrate#2392]
  exit: 'the re-pin onto a release carrying agent-substrate/substrate#2392, with the node image caches wiped there'
  class: 5
  destination: 'agent-substrate/substrate#2392'
  first_step: 'none; wait for agent-substrate/substrate#2392; at its re-pin compare its image cache version with this commit''s whiteouts.json version 2 and wipe node caches (agent-substrate/substrate)'
  source: 'giantswarm @ 5bdf86853'
  test: 'the golden boot of an image with USER 65532 writing its own /config'
  recheck: 'each re-pin'
  owner: unassigned
  draft: ''
  decision: issuecomment-6301319662
  couplings: [golden-boot]
  open_question: ''
depends_on: [actors-run-as-their-images-user-workdir]
verified: 2026-10-21
---
# Image-layer files keep the owner their layer tar gives them

## Change

Image-layer files keep the owner their layer tar gives them (atelet records the tar's non-root owners, with the setuid/setgid mode a chown clears, in the layer metadata `whiteouts.json` from its `version` 2; ateom's `FinalizeLayer` applies them to the pooled layer tree with `lchown` under its own `owners-applied` marker, once per layer node-wide, so the merged rootfs presents them with no copy-up; a layer pooled earlier is backfilled from its tar headers when a pull reuses it)

## Reason

atelet unpacks with every capability dropped, so every image-layer file belonged to root: an actor run as its image's `USER` could not write what its image gave it. kagent's Go ADK image declares `USER 65532` and its golden boot failed with `mkdir /config: permission denied`, then `open /config/config.json: permission denied` once the image created `/config` for its user (giantswarm/kagent-upstream#125)

## History

not ours: [agent-substrate/substrate#2392](https://github.com/agent-substrate/substrate/pull/2392) (author aramase, open, `Fixes #2036`, on top of #1918). Falls away at the re-pin onto the first release that carries #2392

[#115](https://github.com/giantswarm/substrate/pull/115)

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: upstream PR (class 5), another author's: agent-substrate/substrate#2392 carries it with another design, and the file moves to the golden-boot theme.
