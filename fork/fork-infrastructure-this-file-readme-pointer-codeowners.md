---
id: fork-infrastructure-this-file-readme-pointer-codeowners
fork: substrate
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'chore: the boilerplate check skips .json5 as it skips .json'
  - 'chore: declare Renovate off on the fork line'
  - 'fix(sync): name the reason when no upstream pin resolves'
  - 'fix(ci): keep the line''s e2e-test-matrix job key on the re-pin'
  - 'fix(e2e): pass the atespace to the port-in-Host egress probe'
  - 'test(e2e): run the line''s agentgateway in the kind e2e'
  - 'chore(agentgateway): run 2.6.0, which counts refusals per actor'
  - 'chore(agentgateway): run 2.6.1, which counts raw tcp refusals'
  - 'chore(agentgateway): run 2.6.5, which mints through MintActorJwt'
  - 'chore(chart): run the agentgateway line''s 2.3.6 in the router and egress gateway'
  - 'chore(chart): run the agentgateway line''s 2.3.5 in the router and egress gateway'
  - 'chore(chart): run the agentgateway line''s 2.3.0 in the router and egress gateway'
  - 'fix(ci): move the agentgateway-image parameter to 2.2.2 with the chart'
  - 'chore(chart): pin the egress gateway to agentgateway 2.2.2'
  - 'chore(chart): run the agentgateway line''s 2.2.1 in the router and egress'
  - 'chart: run the agentgateway line''s 2.0.0, its first decoupled release on gsoci'
  - 'ci(fork): refuse a release tag on a commit that already carries another version''s tag'
  - 'ci(fork): the ledger cites public repositories only'
  - 'ci(fork): remove upstream''s agentgateway-image.yaml; fail a workflow naming ghcr.io'
  - 'ci(e2e): upload each pod''s whole log when the e2e job fails'
  - 'fix(sync): delete the candidates a re-pin supersedes'
  - 'fix(sync): land a re-pin as the candidate its checks passed on'
  - 'fix(ci): validate skips the tagged maintenance branches'
  - 'ci: the boilerplate check skips the aligned cliff.toml'
  - 'ci: pin giantswarm/architect orb 10.10.0'
  - 'ci: skip the alignment-managed files in the boilerplate check'
  - 'ci(fork): the sync cancels the runs the mirror starts and opens hand-over PRs in this repository'
  - 'ci(fork): the sync workflow pushes as the HeraldBot GitHub App'
  - 'Bootstrap the Giant Swarm line: CI on the giantswarm branch, publishing to ghcr.io/giantswarm/substrate, upstream sync, ledger'
  - 'test: throwaway private link, reverted in the next commit'
  - 'ci(fork): one blank line after the header'
  - 'ci(fork): the ledger row of the public-links check'
  - 'fix(sync): resolve a prerelease pin as the line''s current pin'
  - 'ci(fork): the public-links scripts carry the whole license header'
  - 'ci(fork): the check also fails a row of a private list cited in prose'
  - 'ci(fork): remove upstream''s release.yaml from the consumed branch'
  - 'ci(fork): a repository that is not found is named so; drop the throwaway line'
  - 'fork(publish): atenet-router and atenet-egress run the Giant Swarm line of agentgateway'
  - 'fork(publish): pin the agentgateway line''s v1.5.1-gs.2 — CONNECT-time authorization that admits a resuming actor'
  - 'fork(publish): pin the agentgateway line''s v1.5.1-gs.4 — upstream main ≥ 9f9744cf, the router protocol of kagent-dev/substrate v0.0.29'
  - 'ci(fork): run the suites on sync/** pushes'
  - 'ci(fork): publish the line''s images and charts to gsoci from CircleCI'
  - 'ci: publish through the orb''s stock jobs with decoupled versions'
  - 'fork(publish): re-pin onto kagent-dev/substrate v0.2.0-beta4 — the credential provider image, the registry/repository stamp, the agentgateway line''s build'
  - 'fork(publish): the atenet data plane at the agentgateway line''s 2.1.2'
  - 'fork(publish): the atenet data plane at the agentgateway line''s 2.2.0'
  - 'ci(fork): the pin annotation names kagent-dev/substrate v0.3.0-alpha3'
  - 'ci: lint FORK.md''s ledger on every pull request against a line'
  - 'ci: provide hack/tools/yamllint and lint the repo''s workflow files'
  - 'ci(fork): remove upstream''s release-notes workflow from the consumed branch'
  - 'ci(fork): report one govulncheck status over the module matrix'
  - 'ci: fix the workflow indentation yamllint reports'
  - 'ci(fork): accept a landed candidate GitHub already shows merged'
  - 'ci(fork-ledger): report on re-pin candidate heads and wait for it at the land'
  - 'ci: run helm-verify on a push to giantswarm and the re-pin candidates'
  - 'ci(fork): check the ledger header schema'
upstream:
  outcome: fork-only
  refs: []
  exit: none; says six ko images but the line now publishes kubernetes-secrets and ate-idp-server too
depends_on: []
verified: 2026-10-19
---
# Fork infrastructure

## Change

Fork infrastructure: `FORK.md`, the `fork/` ledger, the README pointer, `CODEOWNERS`, `.circleci/config.yml` (the publish to gsoci from CircleCI: the six ko images for both platforms, the Trivy gate, the two charts, everything signed with the organisation's CircleCI identity; with the `agentgateway-image` pin and the drift check against the chart default — nothing publishes from GitHub Actions and nothing pushes to ghcr.io; upstream's dispatch-only `release.yaml` and `agentgateway-image.yaml` are removed, and `fork-ledger.yaml` fails a tree with a workflow naming ghcr.io), `.github/workflows/sync-upstream.yaml`, `.trivyignore`, and the branch triggers of `pr-workflow.yaml`, `helm-e2e.yaml`, `govulncheck.yaml` and `helm-verify.yaml` ([#301](https://github.com/giantswarm/substrate/pull/301)) (`main` → `giantswarm` and `sync/**` — a re-pin candidate is a rebased branch, so a pull request from it runs no `pull_request` workflow; the push runs the suites and the chart check — govulncheck also on pull requests); the chart default `images.agentgateway` and its comment (`7a1263d0`, the rest of #9 after upstream #28 made the same frontend-policy move); `hack/util/verify-boilerplate.py` skips the files the organisation's repository alignment writes here (`.github/workflows/zz_generated.*`, `.pre-commit-config.yaml`, `.yamllint.yaml`, `.nancy-ignore.generated`, `DCO`), which carry no license header; the ledger check `.github/workflows/fork-ledger.yaml` with `.github/scripts/fork-ledger.sh` and its fixture runs ([#206](https://github.com/giantswarm/substrate/pull/206)), its step keeping the ledger's citations public, `.github/scripts/fork-public-links.sh` with its fixture runs (#454), the land accepting a candidate GitHub already shows merged ([#222](https://github.com/giantswarm/substrate/pull/222)), the publish pipeline's `release-tag` job refusing a release tag whose commit carries another version's release tag, `.github/scripts/release-tag.sh` with its fixture runs (#460)

## Reason

the line's CI, publishing and sync

## History

not for upstream

the `giantswarm` branch history; also [#460](https://github.com/giantswarm/substrate/pull/460) (`ci(fork): refuse a release tag on a commit that already carries another version's tag`), [#47](https://github.com/giantswarm/substrate/pull/47) (`ci(fork): remove upstream's release.yaml; docs(fork): release table v0.0.30-gs.5`), [#454](https://github.com/giantswarm/substrate/pull/454) (`ci(fork): the ledger cites public repositories only`), [#428](https://github.com/giantswarm/substrate/pull/428) (`ci(fork): remove upstream's agentgateway-image.yaml; fail a workflow naming ghcr.io`), [#197](https://github.com/giantswarm/substrate/pull/197) (`ci(e2e): upload each pod's whole log when the e2e job fails`), [#192](https://github.com/giantswarm/substrate/pull/192) (`fix(sync): delete the candidates a re-pin supersedes`), [#185](https://github.com/giantswarm/substrate/pull/185) (`fix(sync): land a re-pin as the candidate its checks passed on`), [#181](https://github.com/giantswarm/substrate/pull/181) (`fix(ci): validate skips the tagged maintenance branches`), [#97](https://github.com/giantswarm/substrate/pull/97) (`ci: the boilerplate check skips the aligned cliff.toml`), [#93](https://github.com/giantswarm/substrate/pull/93) (`fix(deps): otlploggrpc v0.21.0 for GO-2026-6508`), [#67](https://github.com/giantswarm/substrate/pull/67) (`ci: pin giantswarm/architect orb 10.10.0`), [#62](https://github.com/giantswarm/substrate/pull/62) (`ci: skip the alignment-managed files in the boilerplate check`), [#3](https://github.com/giantswarm/substrate/pull/3) (`ci(fork): the sync cancels the runs the mirror starts and opens hand-over PRs in this repository`), [#2](https://github.com/giantswarm/substrate/pull/2) (`ci(fork): the sync workflow pushes as the HeraldBot GitHub App`), [#1](https://github.com/giantswarm/substrate/pull/1) (`Bootstrap the Giant Swarm line: CI on the giantswarm branch, publishing to ghcr.io/giantswarm/substrate, upstream sync, ledger`)
