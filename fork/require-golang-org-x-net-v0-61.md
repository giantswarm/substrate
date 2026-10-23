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
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#2401, agent-substrate/substrate#2984, agent-substrate/substrate#3013]
  exit: 'the x/net v0.61.0 commit is not replayed at the next re-pin (fixed in v0.60.0, which upstream requires); the Go pin commit drops, with pin-go-1-27-3, at the re-pin onto a commit containing 3c9a3cd3 (agent-substrate/substrate#2984: toolchain go1.27.3 in every module, go 1.27.0 kept); the .circleci builder image is the line''s own'
  class: 2
  destination: 'agent-substrate/substrate#2984 (3c9a3cd3, on upstream main)'
  first_step: 'none; the one-line toolchain pull request drafted on 2026-10-20 is withdrawn'
  source: n/a
  test: 'the govulncheck workflow is green on the re-pin'
  recheck: 'at the re-pin'
  owner: unassigned
  draft: ""
  decision: issuecomment-6301335159
depends_on: []
verified: 2026-10-23
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

2026-10-23: upstream has it: agent-substrate/substrate#2984 (merged 2026-10-20, `3c9a3cd3`, the commit the review read) added `toolchain go1.27.3` to every module and the benchmarking Dockerfiles with `go 1.27.0` kept; the one-line pull request drafted on 2026-10-20 is withdrawn, nothing to file. Class 2: the Go pin commit drops, with pin-go-1-27-3, at the re-pin onto a commit containing `3c9a3cd3`. Upstream automates the next patch in agent-substrate/substrate#3013: #3021 (a verify script that the root and plugin `toolchain` lines agree) and #3025 (`.github/renovate.json5`, the `toolchain` directive of the root and `internal/plugins/*` modules only, no `go` directive, no `hack/tools`, no Dockerfiles), both open on that date and waiting for the Renovate app to be installed on the org. The line's root `renovate.json5` (`enabled: false`) comes before `.github/renovate.json5` in Renovate's config-file order, so the re-pin that brings #3025 keeps Renovate off on the line.
