# helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 …

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/helm-verify-pin-setup-helm`](https://github.com/giantswarm/substrate/tree/upstream/helm-verify-pin-setup-helm) here (`96c6557e`, on kagent-dev/substrate `main` @ `9c4b1fb5`, DCO-signed; its message is the pull-request text). A team member opens the kagent-dev/substrate pull request. Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#299](https://github.com/giantswarm/substrate/pull/299): `git cherry-pick -x` of `96c6557e`

## Change

helm-verify pins `actions/checkout` to its v5.1.0 release and `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every run (`with: version: v4.3.0`), the pins of helm-e2e

## Reason

the actions ran unpinned and without a Helm version, the exposure helm-e2e had ([#246](https://github.com/giantswarm/substrate/issues/246)): `helm lint` and `make verify-helm-template` could run on another Helm major version than helm-e2e ([#280](https://github.com/giantswarm/substrate/issues/280)). Workflow-only
