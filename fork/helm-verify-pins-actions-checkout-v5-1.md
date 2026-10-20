---
id: helm-verify-pins-actions-checkout-v5-1
fork: substrate
theme: ci-publish-infra
category: pin
status: carried
commits:
  - 'ci: pin checkout, setup-helm and the Helm version in helm-verify'
upstream:
  outcome: to-file
  refs: [kagent-dev/substrate#86]
  exit: 'the Helm version and checkout pull requests merged, then the Substrate re-pin drops the commit; a declined checkout pull request makes that half fork-only, with no functional effect'
  class: 5
  destination: 'kagent-dev/substrate helm-verify.yaml:30-31: the Helm version with the helm-e2e row, actions/checkout v5.1.0 as its own lower-priority pull request; the SHA pins are upstream since #86'
  first_step: 'open the checkout pull request on kagent-dev/substrate after the Helm version pull request'
  source: 'upstream/helm-verify-checkout-v5@df92b6a48 (on 6383ea118, +1/-1); the Helm version is upstream/helm-version-pin@fd43d8446; upstream/helm-verify-pin-setup-helm@96c6557e0 is retired'
  test: 'helm lint and hack/render-manifests.sh --check pass in helm-verify on Helm v4.3.1 with actions/checkout v5.1.0'
  recheck: 'each Substrate re-pin'
  owner: unassigned
  draft: issuecomment-6301362710
  decision: issuecomment-6301358702
  couplings: [compat-migrations]
depends_on: [helm-e2e-pins-azure-setup-helm-v4]
verified: 2026-10-20
---
# helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 …

## Change

helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every run (`with: version: v4.3.0`), the pins of helm-e2e

## Reason

the actions ran unpinned and without a Helm version, the exposure helm-e2e had ([#246](https://github.com/giantswarm/substrate/issues/246)): `helm lint` and `make verify-helm-template` could run on another Helm major version than helm-e2e ([#280](https://github.com/giantswarm/substrate/issues/280)). Workflow-only

## History

to file: the upstream-shaped patch is branch [`upstream/helm-verify-pin-setup-helm`](https://github.com/giantswarm/substrate/tree/upstream/helm-verify-pin-setup-helm) here (`96c6557e`, on kagent-dev/substrate `main` @ `9c4b1fb5`, DCO-signed; its message is the pull-request text). A team member opens the kagent-dev/substrate pull request. Falls away at the re-pin onto the first release that carries it

[#299](https://github.com/giantswarm/substrate/pull/299): `git cherry-pick -x` of `96c6557e`

2026-10-20: placed by the T16 ci-publish-infra theme review (the decision comment is `upstream.decision` on the theme issue): SHA pins upstream (kagent-dev/substrate#86); upstream's helm-verify runs `hack/render-manifests.sh --check` where the line still runs `make verify-helm-template`.
