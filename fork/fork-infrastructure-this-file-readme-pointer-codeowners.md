# Fork infrastructure

- **Status:** carried
- **Upstream:** not for upstream
- **Our commit:** the `giantswarm` branch history

## Change

Fork infrastructure: `FORK.md`, the `fork/` ledger, the README pointer, `CODEOWNERS`, `.circleci/config.yml` (the publish to gsoci from CircleCI: the six ko images for both platforms, the Trivy gate, the two charts, everything signed with the organisation's CircleCI identity; with the `agentgateway-image` pin and the drift check against the chart default — nothing publishes from GitHub Actions and nothing pushes to ghcr.io; upstream's dispatch-only `release.yaml` is removed), `.github/workflows/sync-upstream.yaml`, `.trivyignore`, and the branch triggers of `pr-workflow.yaml`, `helm-e2e.yaml`, `govulncheck.yaml` and `helm-verify.yaml` ([#301](https://github.com/giantswarm/substrate/pull/301)) (`main` → `giantswarm` and `sync/**` — a re-pin candidate is a rebased branch, so a pull request from it runs no `pull_request` workflow; the push runs the suites and the chart check — govulncheck also on pull requests); the chart default `images.agentgateway` and its comment (`7a1263d0`, the rest of #9 after upstream #28 made the same frontend-policy move); `hack/util/verify-boilerplate.py` skips the files giantswarm/github's alignment writes here (`.github/workflows/zz_generated.*`, `.pre-commit-config.yaml`, `.yamllint.yaml`, `.nancy-ignore.generated`, `DCO`), which carry no license header; the ledger check `.github/workflows/fork-ledger.yaml` with `.github/scripts/fork-ledger.sh` and its fixture runs ([#206](https://github.com/giantswarm/substrate/pull/206)), the land accepting a candidate GitHub already shows merged ([#222](https://github.com/giantswarm/substrate/pull/222))

## Reason

the line's CI, publishing and sync
