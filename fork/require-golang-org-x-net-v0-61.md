---
id: require-golang-org-x-net-v0-61
fork: substrate
theme: version-pins
category: pin
status: carried
upstream:
  outcome: drops-at-repin
  refs: []
  exit: upstream bumps x/net
depends_on: []
commit: giantswarm/substrate#252
verified: 2026-10-19
---
# Require `golang.org/x/net` v0.61.0 in every module that had it below

- **Status:** carried
- **Upstream:** not for upstream as such: changes versions only and falls away at the re-pin onto the first upstream commit that requires `x/net` ≥ v0.61.0 (upstream's dependency automation moves it)
- **Our commit:** [#252](https://github.com/giantswarm/substrate/pull/252) (`fix(deps): bump golang.org/x/net to v0.61.0 for two HTTP/2 CVEs`)

## Change

Require `golang.org/x/net` v0.61.0 in every module that had it below (`go.mod`, `hack/tools/{code-generator,controller-gen,go-licenses,ko}`, `internal/plugins/gcp-secret-manager`, `tools/apitool`; `go get golang.org/x/net@v0.61.0 && go mod tidy` in each, then `go mod vendor` for the root module the CircleCI builds compile with `-mod=vendor`; `x/sync`, `x/sys`, `x/term`, `x/text` and `x/tools` move along, and tidy drops the stale indirect requirements `code-generator`'s module file still listed)

## Reason

CVE-2026-97032 and CVE-2026-78663, two HTTP/2 vulnerabilities in `x/net` below v0.61.0; the platform's Go repositories move together, their CircleCI nancy step refusing the older module
