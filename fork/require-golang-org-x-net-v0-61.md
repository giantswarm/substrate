---
id: require-golang-org-x-net-v0-61
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): bump x/net to v0.60.0 and pin Go 1.27.2'
  - 'fix(deps): bump golang.org/x/net to v0.61.0 for two HTTP/2 CVEs'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2401, agentgateway/agentgateway#4063]
  exit: 'the x/net v0.61.0 commit is not replayed at the next re-pin; the Go pin commit is reshaped on the line to toolchain go1.27.3 (go directive back to 1.27.0, .circleci/Dockerfile builder golang:1.27.3, govulncheck lane on the action''s stable) and filed on agent-substrate/substrate as one line after the kagent PR; if declined: dropped, the fork infra carries the version'
  class: 5
  destination: agent-substrate/substrate
  first_step: 'fork infra first: reshape the Go pin commit to toolchain go1.27.3 with go 1.27.0, builder golang:1.27 -> golang:1.27.3, lane on the action''s stable, and split this file; then one line on agent-substrate/substrate. The x/net v0.61.0 commit is class 1 and is not replayed at the next re-pin'
  source: 'new branch upstream/toolchain-go1-27-3 on giantswarm/substrate, base agent-substrate main at or past 3c9a3cd3'
  test: 'the govulncheck matrix stays clean on the file''s toolchain; go build ./... with a local go1.27.0 fetches 1.27.3'
  recheck: 2026-11-17
  owner: unassigned
  draft: issuecomment-6301335986
  decision: issuecomment-6301335159
depends_on: []
verified: 2026-10-20
---
# Require `golang.org/x/net` v0.61.0 in every module that had it below

## Change

Require `golang.org/x/net` v0.61.0 in every module that had it below (`go.mod`, `hack/tools/{code-generator,controller-gen,go-licenses,ko}`, `internal/plugins/gcp-secret-manager`, `tools/apitool`; `go get golang.org/x/net@v0.61.0 && go mod tidy` in each, then `go mod vendor` for the root module the CircleCI builds compile with `-mod=vendor`; `x/sync`, `x/sys`, `x/term`, `x/text` and `x/tools` move along, and tidy drops the stale indirect requirements `code-generator`'s module file still listed)

## Reason

GO-2026-6612 and GO-2026-6617 (CVE-2026-78663, CVE-2026-97032), two HTTP/2 vulnerabilities in `x/net` below v0.60.0, which agent-substrate/substrate#2401 brought upstream on 2026-10-10; the v0.61.0 half was read as needed for them and is not. The Go 1.27.2 directive moved the line's own builds and the `govulncheck` workflow (which reads `go.mod` on the line, `go-version-input: ""`) onto the October security release; upstream's workflow runs on the action's `stable` toolchain instead. The line has no nancy step: its gates are the trivy image scan and `govulncheck`

## History

not for upstream as such: changes versions only and falls away at the re-pin onto the first upstream commit that requires `x/net` ≥ v0.61.0 (upstream's dependency automation moves it)

[#252](https://github.com/giantswarm/substrate/pull/252) (`fix(deps): bump golang.org/x/net to v0.61.0 for two HTTP/2 CVEs`)

2026-10-20: decided in the version-pins theme review (decision issuecomment-6301335159): the x/net v0.61.0 commit is class 1 drop (fixed in v0.60.0, which upstream requires; no consumer); the Go pin commit is class 5 upstream PR, uncapped, queued after the kagent one: upstream runs govulncheck on the action's stable (run 39410251559, go1.27.2, clean) and #2401 preserved the go requirement, so the upstream shape is a `toolchain go1.27.3` line, which applies to the main module only; the line's `go 1.27.2` directive is also what forces the kagent line's go directive through its replace, so the reshape precedes the kagent mirror.
