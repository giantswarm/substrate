---
id: require-go-jose-v4-1-8
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): bump github.com/go-jose/go-jose/v4 to v4.1.8 for CVE-2026-116587'
upstream:
  outcome: drops-at-repin
  refs: []
  exit: 'the re-pin onto an upstream commit that requires github.com/go-jose/go-jose/v4 v4.1.6 or later does not replay the commit'
depends_on: []
verified: 2026-11-05
---
# Require `github.com/go-jose/go-jose/v4` v4.1.8

## Change

Require `github.com/go-jose/go-jose/v4` v4.1.8 in the root module (vendored) and `hack/tools/ko`, both indirect (`go get github.com/go-jose/go-jose/v4@v4.1.8 && go mod tidy`, then `go mod vendor` for the root module). Nothing else moves.

## Reason

CVE-2026-116587 (HIGH, a denial of service, fixed in 4.1.6) in v4.1.4, which ate-api reaches through `github.com/spiffe/go-spiffe/v2`. The trivy scan of the CircleCI publish fails on it for the `ateapi` and `atelet` images, so no release from 1.18.0-rc.4 on published its images' charts.

## History

not for upstream as such: changes versions only. On 2026-11-05 agent-substrate/substrate and kagent-dev/substrate `main` both require v4.1.4.

[#556](https://github.com/giantswarm/substrate/pull/556) (`fix(deps): bump github.com/go-jose/go-jose/v4 to v4.1.8 for CVE-2026-116587`)
