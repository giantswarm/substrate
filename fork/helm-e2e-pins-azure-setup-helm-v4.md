---
id: helm-e2e-pins-azure-setup-helm-v4
fork: substrate
theme: ci-publish-infra
category: pin
status: carried
commits:
  - 'ci: pin setup-helm and the Helm version in helm-e2e'
upstream:
  outcome: open
  refs: [kagent-dev/substrate#86, kagent-dev/substrate#96]
  exit: 'kagent-dev/substrate#96 merged; the Substrate re-pin onto a kagent-dev/substrate commit carrying it drops the commit'
  class: 5
  destination: 'kagent-dev/substrate#96 (version input on azure/setup-helm in helm-e2e.yaml and helm-verify.yaml); the SHA pin half is upstream since kagent-dev/substrate#86 (9d1968a9)'
  first_step: 'mark kagent-dev/substrate#96 ready for review'
  source: 'upstream/helm-version-pin@fd43d8446 (+4, merges cleanly on kagent-dev/substrate main); upstream/helm-e2e-pin-setup-helm@5b47d3578 is retired (it conflicts with #86)'
  test: 'the helm-e2e and helm-verify logs show Helm v4.3.1; azure/setup-helm v4.3.1 without a version falls back to v3.18.4 when its latest-version lookup fails (src/run.ts:13,49-59)'
  recheck: '2026-11-17, then each Substrate re-pin'
  owner: QuentinBisson
  draft: issuecomment-6301362157
  decision: issuecomment-6301358702
  couplings: [compat-migrations]
depends_on: []
verified: 2026-10-23
---
# helm-e2e pins `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every …

## Change

helm-e2e pins `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every run (`with: version: v4.3.0`)

## Reason

the action ran without a version and installed whatever Helm it resolved that day: a run got Helm 3.18.4 instead of Helm 4, and the step's `helm plugin install … --verify=false`, a Helm 4 flag, failed with `unknown flag: --verify` until a rerun ([#246](https://github.com/giantswarm/substrate/issues/246)). Workflow-only

## History

to file: the upstream-shaped patch is branch [`upstream/helm-e2e-pin-setup-helm`](https://github.com/giantswarm/substrate/tree/upstream/helm-e2e-pin-setup-helm) here (`5b47d357`, on kagent-dev/substrate `main` @ `9c4b1fb5`, DCO-signed; its message is the pull-request text). kagent-dev/substrate's own helm-e2e carries the same unpinned step; agent-substrate/substrate has no helm-e2e. A team member opens the kagent-dev/substrate pull request. Falls away at the re-pin onto the first release that carries it

[#275](https://github.com/giantswarm/substrate/pull/275): `git cherry-pick -x` of `5b47d357`, adapted to the line's indentation

2026-10-20: placed by the T16 ci-publish-infra theme review (the decision comment is `upstream.decision` on the theme issue): the SHA pin is upstream (kagent-dev/substrate#86, `9d1968a9`); the Helm version goes upstream as its own pull request; the commit rides the Substrate conversion re-pin of the compat-migrations theme.

2026-10-23: opened as kagent-dev/substrate#96 (draft) from `upstream/helm-version-pin` (`fd43d844`); Helm v4.3.1 is still the latest Helm 4 release.
