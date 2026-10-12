# helm-e2e's failure dump names a restarted container's end

- **Status:** carried
- **Upstream:** to file: kagent-dev/substrate's helm-e2e carries the same dump step; agent-substrate/substrate has no helm-e2e. Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#362](https://github.com/giantswarm/substrate/pull/362)

## Change

On failure, helm-e2e's dump prints every restarted container's termination reason, exit code and finish time, the previous run's log of each `ate-system` pod that has one, and the cluster's warning events.

## Reason

A component that restarts mid-suite (the node's atelet, by a crash or an OOM kill) left only its new run's log in the dump, so the failures it caused carried no cause: the micro-VM lane failed for a day on an atelet restart whose reason no log showed ([#357](https://github.com/giantswarm/substrate/issues/357)). Workflow-only
