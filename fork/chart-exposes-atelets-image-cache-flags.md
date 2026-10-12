# The chart exposes atelet's image-cache flags

- **Status:** carried
- **Upstream:** with the row above on `upstream/atelet-image-cache-pinned-images` (`4c2f33ea`; the scheduling knobs of the carried patch above are not on that branch)
- **Our commit:** `686b7835`, [#21](https://github.com/giantswarm/substrate/pull/21)

## Change

The chart exposes atelet's image-cache flags (`atelet.imageCache.{gcPeriod,highPercent,lowPercent,minAge,maxBytes,pinnedImages}`; every key null/empty leaves atelet's built-in default)

## Reason

the platform pins its pool's Harness images and raises the watermarks on nodes that share the cache volume without hand-writing `atelet.extraArgs`; the meta chart sets `pinnedImages` through this key
