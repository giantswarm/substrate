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
  refs: []
  exit: upstream requires Go 1.27.3 or later
depends_on: [require-golang-org-x-net-v0-61]
verified: 2026-10-21
---
# Pin Go 1.27.3

## Change

The `go` directive of every module that named 1.27.2 (`go.mod`, `hack/tools/{golangci-lint,kube-api-linter}`, `internal/plugins/gcp-secret-manager`, `tools/apitool`) names 1.27.3. The workflows read the Go to install from `go.mod`, and the CircleCI builder's `golang:1.27` image already resolves to it.

## Reason

GO-2026-6809, a vulnerability in `crypto/x509` of Go below 1.27.3, reached from atenet's egress through `verifyActorCertificate` and from the GCP Secret Manager credential provider; `govulncheck`, a required check, fails every pull request against the line until the line builds with 1.27.3.

## History

not for upstream as such: changes the Go version only and falls away at the re-pin onto the first upstream commit that requires Go 1.27.3 or later
