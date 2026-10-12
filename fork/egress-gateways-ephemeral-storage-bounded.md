# The egress gateway's ephemeral storage is bounded

- **Status:** carried
- **Upstream:** to file: upstream's `atenet-egress.yaml` renders the same unbounded shape and no upstream issue covers it; queued in the upstream engagement list
- **Our commit:** `9246909a` ([#255](https://github.com/giantswarm/substrate/pull/255), rebase-merged 2026-10-10; first release 1.8.0-rc.4)

## Change

The egress gateway's ephemeral storage is bounded (`atenetEgress.resources` for the `agentgateway` container and `atenetEgress.extProc.resources`, ephemeral-storage requests `16Mi` and limits `256Mi` by default, CPU and memory left to the operator; `atenetEgress.drainSignal.sizeLimit`, `16Mi` on the `drain-signal` emptyDir the ext-proc writes its drain marker to; `null` renders the field as before; `charts/substrate/tests/atenet_egress_ephemeral_storage_test.yaml`; README rows)

## Reason

the chart rendered both containers without `resources` and the emptyDir without a `sizeLimit`, so a cluster policy that requires bounded ephemeral storage for a container mounting an emptyDir (Kyverno's `require-emptydir-requests-and-limits`) reported the egress gateway on every install ([giantswarm/agent-platform#880](https://github.com/giantswarm/agent-platform/issues/880))
