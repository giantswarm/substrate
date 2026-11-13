---
id: pin-go-1-27-3
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): pin Go 1.27.3 for GO-2026-6809'
  - 'fix(deps): pin Go 1.27.4 for GO-2026-6859'
upstream:
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#2984]
  exit: 'drops with require-golang-org-x-net-v0-61 at the re-pin onto the first upstream commit whose toolchain line in every module names go1.27.4 or later (agent-substrate/substrate#2984 named go1.27.3)'
depends_on: [require-golang-org-x-net-v0-61]
verified: 2026-11-13
---
# Pin Go 1.27.4

## Change

The `go` directive of every module that named 1.27.2 (`go.mod`, `hack/tools/{golangci-lint,kube-api-linter}`, `internal/plugins/gcp-secret-manager`, `tools/apitool`) names 1.27.4. The workflows read the Go to install from `go.mod`, and the CircleCI builder's `golang:1.27` image resolves to it, or fetches it through the default `GOTOOLCHAIN=auto`.

## Reason

GO-2026-6809, a vulnerability in `crypto/x509` of Go below 1.27.3, reached from atenet's egress through `verifyActorCertificate` and from the GCP Secret Manager credential provider, and GO-2026-6859, a vulnerability in `net/http` of Go below 1.27.4, reached from the root module and the GCP Secret Manager plugin; `govulncheck`, a required check, fails every pull request against the line until the line builds with 1.27.4.

## History

not for upstream as such: changes the Go version only and falls away at the re-pin onto the first upstream commit that requires Go 1.27.3 or later

[#527](https://github.com/giantswarm/substrate/pull/527)

2026-10-23: agent-substrate/substrate#2984 (`3c9a3cd3`) is the upstream commit this falls away at; the line's `go 1.27.3` directive and upstream's `toolchain go1.27.3` select the same Go.

2026-11-13: GO-2026-6859 moves the pin to 1.27.4; upstream's `toolchain go1.27.3` no longer suffices, so the pin falls away only at an upstream commit naming go1.27.4 or later.
