# The atelet DaemonSet takes `nodeSelector`, `tolerations` and `affinity`

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/atelet-scheduling`](https://github.com/giantswarm/substrate/tree/upstream/atelet-scheduling) here (`b34c1690`, on the mirror `main`); [kagent-dev/substrate#16](https://github.com/kagent-dev/substrate/pull/16) touches the same knob (`atelet.nodeSelector`, no tolerations or affinity) inside a fork-wide 92-file pull request that has conflicted since July — align with the maintainers there; re-checked beside agent-substrate/substrate#1874 (tolerations for workload types, merged 2026-09-25, in v0.3.0-alpha1) and alpha1's fixed `ate.dev/sandboxClass` toleration (`80f37cf6`) at the re-pin; a team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it
- **Our commit:** `931f9ed4`, [#8](https://github.com/giantswarm/substrate/pull/8)

## Change

The atelet DaemonSet takes `nodeSelector`, `tolerations` and `affinity` (`atelet.{nodeSelector,tolerations,affinity}`, empty by default)

## Reason

the platform pins atelet to worker nodes / node pools; the chart had no scheduling knob ([giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342))
