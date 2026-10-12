# atelet pins images the cache must always hold

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/atelet-image-cache-pinned-images`](https://github.com/giantswarm/substrate/tree/upstream/atelet-image-cache-pinned-images) here (`f6757c3a` + `4c2f33ea`, on the mirror `main` @ `007eb1ee`; the messages are the pull-request text). agent-substrate/substrate#1383 (open, 2026-09-02) is this exact failure — a cold actor-image cache wedges a request-driven resume — and #811 the same shape through the sandbox-asset cache; the eviction engine (#735, #836, design #463) pins or spares nothing. A team member opens the pull request with DCO sign-off once the team has reviewed it
- **Our commit:** `ba39208a`, [#21](https://github.com/giantswarm/substrate/pull/21) (rebase-merged 2026-09-12)

## Change

atelet pins images the cache must always hold (`--image-cache-pinned-images`: every GC pass pulls each pinned reference that is not cached and roots it — record, layers and the multi-arch twin — like a placed actor's; a failed pull is logged and retried next pass without gating the pass; with the pass disabled the pins are pulled once at start; `imagecache.Store.Pin` is in-memory, re-pinned after a restart)

## Reason

the eviction pass protects only placed actors' images and anything younger than `--image-cache-min-age`; on a node whose cache volume sits above `--image-cache-high-percent` for reasons other than the cache (a shared root volume, foreign disk pressure) every pass evicted every image older than two minutes, the cache ran at a permanent 0 % hit rate and every first actor after an idle spell paid a cold pull and unpack (~5 s for a 1 GiB harness image) on the resume path — the first half of the lab's hanging turns
