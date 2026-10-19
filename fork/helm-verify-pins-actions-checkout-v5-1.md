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
  refs: []
  exit: kd PR
depends_on: []
verified: 2026-10-19
---
# helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 …

## Change

helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every run (`with: version: v4.3.0`), the pins of helm-e2e

## Reason

the actions ran unpinned and without a Helm version, the exposure helm-e2e had ([#246](https://github.com/giantswarm/substrate/issues/246)): `helm lint` and `make verify-helm-template` could run on another Helm major version than helm-e2e ([#280](https://github.com/giantswarm/substrate/issues/280)). Workflow-only

## History

to file: the upstream-shaped patch is branch [`upstream/helm-verify-pin-setup-helm`](https://github.com/giantswarm/substrate/tree/upstream/helm-verify-pin-setup-helm) here (`96c6557e`, on kagent-dev/substrate `main` @ `9c4b1fb5`, DCO-signed; its message is the pull-request text). A team member opens the kagent-dev/substrate pull request. Falls away at the re-pin onto the first release that carries it

[#299](https://github.com/giantswarm/substrate/pull/299): `git cherry-pick -x` of `96c6557e`
