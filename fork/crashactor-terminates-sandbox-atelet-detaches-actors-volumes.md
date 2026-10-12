# `crashActor` terminates the sandbox on atelet, detaches the actor's volumes and only then releases …

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#1953](https://github.com/agent-substrate/substrate/pull/1953) (open: the crash tears the workload down before `CRASHED` and keeps the assignment when it fails, the shape adapted here) and [agent-substrate/substrate#1972](https://github.com/agent-substrate/substrate/pull/1972) (open: refuses the DATA scope without a durable dir, for agent-substrate/substrate#1936); falls away at the re-pin onto the first release that carries both
- **Our commit:** [#258](https://github.com/giantswarm/substrate/pull/258) (`fix(ateapi): tear a crashed actor's workload down before it is deleted`)

## Change

`crashActor` terminates the sandbox on atelet, detaches the actor's volumes and only then releases the worker (a failed step leaves the actor `CRASHED` with its worker assignment, for `DeleteActor` or `RevertActor` to retry); `DeleteActor` refuses with `FailedPrecondition` while the sandbox cannot be stopped or a volume released, keeping the assignment for the retry; `CreateActorTemplate` refuses a DATA `on_pause` or `on_commit` scope unless a container mounts a durable-dir volume (`InvalidArgument` naming the field); the existing-volumes e2e suite covers the delete after a failed suspend

## Reason

a failed checkpoint crashed the actor and released its worker with the sandbox still running on it, so `DeleteActor` found no worker to reach and removed the record: the sandbox, its existing-volume NFS mounts and the worker's capacity leaked until the worker pod restarted, and the PersistentVolume stayed `Released` on `directory not empty` ([giantswarm/substrate#237](https://github.com/giantswarm/substrate/issues/237))
