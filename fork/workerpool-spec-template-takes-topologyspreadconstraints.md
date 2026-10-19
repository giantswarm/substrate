---
id: workerpool-spec-template-takes-topologyspreadconstraints
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'feat(workerpool): podAffinity on the worker pod template'
  - 'WorkerPool: topologySpreadConstraints and podAntiAffinity on the pod template'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#212, agent-substrate/substrate#247, agent-substrate/substrate#793, agent-substrate/substrate#819]
  exit: upstream PR
depends_on: []
verified: 2026-10-19
---
# `WorkerPool.spec.template` takes `topologySpreadConstraints`

## Change

`WorkerPool.spec.template` takes `topologySpreadConstraints` (an atomic list of at most 8) and `podAntiAffinity` (mapped onto the worker pod's `spec.topologySpreadConstraints` and next to `nodeAffinity` in one declared `Affinity`, so the controller owns the whole affinity under server-side apply; no default spread; the CRD regenerated with the `substrate-crds` chart's verbatim copy; the api-guide shows a hostname-and-zone spread selecting the pool's own workers by the `ate.dev/worker-pool` label)

## Reason

the ate-api scheduler picks a uniformly random eligible worker and knows nothing about nodes, so a pool's resilience is what the Kubernetes scheduler and the autoscaler make of the pod template — after the CPU-generation pin ([giantswarm/agent-platform#457](https://github.com/giantswarm/agent-platform/issues/457)) Karpenter bin-packed all four `kagent-default` workers onto one spot `m6a.xlarge` (2026-09-15) while two other generation-6 nodes existed: one node loss takes every turn in flight and every paused session; a spread constraint set through the kagent chart's verbatim `substrateWorkerPool.template` was pruned silently by the structural schema and looked applied. The meta chart forwards the two fields behind a render guard from the release that carries them on ([giantswarm/agent-platform#472](https://github.com/giantswarm/agent-platform/issues/472)), the fleet template sets them

## History

to file: the upstream-shaped patch is branch [`upstream/workerpool-spread`](https://github.com/giantswarm/substrate/tree/upstream/workerpool-spread) here (`05f37296`, on the mirror `main` @ `463f483c`; its message is the pull-request text). It answers agent-substrate/substrate#819 item 4 ("a default hostname spread or a documented values knob"; the maintainer on 2026-08-10: makes sense, little energy for it given #793, the node-level-runtime discussion closed without a plan); #212 → #247 shipped `spec.template` without spread, and nothing upstream carries anti-affinity or spread today (kagent-dev/substrate and agent-substrate/substrate searched 2026-09-15). Whether a hostname spread becomes a default is upstream's call — the patch adds the knob only. A team member opens the pull request with DCO sign-off once the team has reviewed it

`d2ba96d2`, [#33](https://github.com/giantswarm/substrate/pull/33) (rebase-merged 2026-09-15; first release v0.0.30-gs.2); also [#98](https://github.com/giantswarm/substrate/pull/98) (`feat(workerpool): podAffinity on the worker pod template`)
