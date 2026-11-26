---
id: require-compress-v1-20-2
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): require compress v1.20.2 for GO-2026-6865'
upstream:
  outcome: drops-at-repin
  refs: []
  exit: re-pin onto an upstream requirement of github.com/klauspost/compress v1.20.2 or later
depends_on: []
verified: 2026-11-07
---
# Require compress v1.20.2

## Change

Require and vendor github.com/klauspost/compress v1.20.2 in the root module.
No other dependency version changes.

## Reason

GO-2026-6865 reports a panic on two-block frames in v1.19.1, reachable
through the benchmark's disk stream and the compression helpers. The
required govulncheck gate refuses that version; v1.20.2 fixes it.

## History

The upstream main requirement is still v1.19.1. This version-only floor
leaves the line at the first re-pin that carries the fixed requirement.

[Pull request](https://github.com/giantswarm/substrate/pull/560).
