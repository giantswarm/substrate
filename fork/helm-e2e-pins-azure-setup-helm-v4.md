# helm-e2e pins `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every …

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/helm-e2e-pin-setup-helm`](https://github.com/giantswarm/substrate/tree/upstream/helm-e2e-pin-setup-helm) here (`5b47d357`, on kagent-dev/substrate `main` @ `9c4b1fb5`, DCO-signed; its message is the pull-request text). kagent-dev/substrate's own helm-e2e carries the same unpinned step; agent-substrate/substrate has no helm-e2e. A team member opens the kagent-dev/substrate pull request. Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#275](https://github.com/giantswarm/substrate/pull/275): `git cherry-pick -x` of `5b47d357`, adapted to the line's indentation

## Change

helm-e2e pins `azure/setup-helm` to its v4.3.1 release by commit and installs Helm v4.3.0 on every run (`with: version: v4.3.0`)

## Reason

the action ran without a version and installed whatever Helm it resolved that day: a run got Helm 3.18.4 instead of Helm 4, and the step's `helm plugin install … --verify=false`, a Helm 4 flag, failed with `unknown flag: --verify` until a rerun ([#246](https://github.com/giantswarm/substrate/issues/246)). Workflow-only
