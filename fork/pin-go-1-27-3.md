---
id: pin-go-1-27-3
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): pin Go 1.27.3 for GO-2026-6809'
upstream:
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#2984]
  exit: 'drops with require-golang-org-x-net-v0-61 at the re-pin onto a commit containing 3c9a3cd3 (agent-substrate/substrate#2984: toolchain go1.27.3 in every module)'
depends_on: [require-golang-org-x-net-v0-61]
verified: 2026-10-23
---
# Pin Go 1.27.3

## Change

The `go` directive of every module that named 1.27.2 (`go.mod`, `hack/tools/{golangci-lint,kube-api-linter}`, `internal/plugins/gcp-secret-manager`, `tools/apitool`) names 1.27.3. The workflows read the Go to install from `go.mod`, and the CircleCI builder's `golang:1.27` image already resolves to it.

## Reason

GO-2026-6809, a vulnerability in `crypto/x509` of Go below 1.27.3, reached from atenet's egress through `verifyActorCertificate` and from the GCP Secret Manager credential provider; `govulncheck`, a required check, fails every pull request against the line until the line builds with 1.27.3.

## History

not for upstream as such: changes the Go version only and falls away at the re-pin onto the first upstream commit that requires Go 1.27.3 or later

[#527](https://github.com/giantswarm/substrate/pull/527)

2026-10-23: agent-substrate/substrate#2984 (`3c9a3cd3`) is the upstream commit this falls away at; the line's `go 1.27.3` directive and upstream's `toolchain go1.27.3` select the same Go.
