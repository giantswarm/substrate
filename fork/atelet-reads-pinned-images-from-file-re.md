---
id: atelet-reads-pinned-images-from-file-re
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'fix(atelet): read pinned images from a file, not the pod template'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR stacked on the pinned-images flag
depends_on: [atelet-pins-images-cache-must-always-hold]
verified: 2026-11-27
---
# atelet reads its pinned images from a file it re-reads at every pass

## Change

atelet reads its pinned images from a file it re-reads at every pass (`--image-cache-pinned-images-file`, every `--image-cache-pinned-images-file-period` with the pass disabled) and unpins an image no listed reference holds any more (`imagecache.Store.Unpin`); the chart renders `atelet.imageCache.pinnedImages` into the ConfigMap `atelet-pinned-images`, mounted whole, and the argument never changes

## Reason

the list follows the kagent chart's runtime images (its `kagent-images` ConfigMap, through the substrate HelmRelease's `valuesFrom`); rendered as an argument, every kagent release changed the atelet pod template and restarted every atelet on every node, and every running session with it, though nothing in Substrate had changed (giantswarm/kagent-upstream#206). Now a new list changes the ConfigMap alone

## History

to file with the rows above: the upstream-shaped patch is branch [`upstream/atelet-pinned-images-file`](https://github.com/giantswarm/substrate/tree/upstream/atelet-pinned-images-file) (`f720fef6`, on `upstream/atelet-image-cache-pinned-images`; the message is the pull-request text)

`ddf1c472`, [#166](https://github.com/giantswarm/substrate/pull/166); on `release-1.3` [#167](https://github.com/giantswarm/substrate/pull/167)

2026-11-27: replayed onto kagent-dev/substrate v1.0.1 (`release-1.0@7ce6f439`); shape on v1.0.1: flags `--image-cache-pinned-images-file[-period]`; value `atelet.imageCache.pinnedImages` → ConfigMap `atelet-pinned-images` (platform feeds it from `kagent-images`).
