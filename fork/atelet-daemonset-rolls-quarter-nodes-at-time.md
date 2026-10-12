# The atelet DaemonSet rolls a quarter of the nodes at a time

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2481](https://github.com/agent-substrate/substrate/pull/2481), from the branch `upstream/atelet-rolling-update-max-unavailable`: the manifest commit, same patch id, which the re-pin drops once upstream carries it. The chart's upstream is kagent-dev/substrate (agent-substrate packages no chart): the chart hunk is to file there with the atelet chart rows above
- **Our commit:** [#313](https://github.com/giantswarm/substrate/pull/313): the fork's own commits, the manifest's (upstream-identical) and the chart's

## Change

The atelet DaemonSet rolls a quarter of the nodes at a time (`atelet.updateStrategy`, the DaemonSet's update strategy rendered verbatim, default `RollingUpdate` with `maxUnavailable: 25%`, a percentage rounded up; a non-zero `maxSurge` fails the render, since atelet's host ports and the node's kubelet plugin sockets admit one atelet per node; the kubectl-apply manifest `manifests/ate-install/atelet.yaml` carries the same default)

## Reason

the chart set no strategy, so Kubernetes rolled one node at a time with no surge, about 20 s per node: a 23-node cluster took about 7.5 minutes, past the 5 minutes helm-controller gives a HelmRelease upgrade by default, and a 1.8.1 → 1.8.2 step was rolled back twice on the deadline ([#312](https://github.com/giantswarm/substrate/issues/312); the timeout's own fix is giantswarm/agent-platform#952). A node's atelet restart costs the sessions on it a retried call while their actors keep running, whatever the number of nodes rolling at once, so a wider roll is a shorter total, not a deeper outage; now 23 nodes roll six at a time in four rounds
