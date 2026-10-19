---
id: every-gvisor-workerpool-runs-releases-own-ateom
fork: substrate
theme: compat-migrations
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: [agent-substrate/substrate#1357]
  exit: fix the consumer-side ordering instead
depends_on: [actors-run-as-their-images-user-workdir]
commit: giantswarm/substrate#114
verified: 2026-10-19
---
# Every gVisor WorkerPool runs the release's own `ateom-gvisor`

- **Status:** carried
- **Upstream:** fork-only: upstream installs pin atelet and ateom to one build per node (agent-substrate/substrate#1357)
- **Our commit:** [#114](https://github.com/giantswarm/substrate/pull/114), [#309](https://github.com/giantswarm/substrate/pull/309)

## Change

Every gVisor WorkerPool runs the release's own `ateom-gvisor` (atecontroller `--gvisor-worker-image` replaces a gVisor pool's `spec.workerImage` in the worker Deployment; the chart sets it to `ateom-gvisor` composed like the other component images, `ateController.gvisorWorker.followRelease`, on by default; micro-VM pools and the `spec.workerImage` field are unchanged; the pool's `status.workerImage` names the image its workers run, and an override is the condition `WorkerImageOverridden=True` (reason `GVisorWorkerImageFlag`, both images in its message) and a `Warning` event `WorkerImageOverridden` on the pool; helm-e2e builds `ateom-gvisor` and points the counter pool at `1.3.0` before the gVisor suites)

## Reason

atelet and ateom share a node-local protocol with no cross-version guarantee, but the platform set the pool's worker to the floor of its Substrate range while Flux moved atelet to the newest release in it: atelet `1.3.1-rc.4` (the `USER` row above) with worker `1.3.0` left the pause container's `/` at `0700`, and every gVisor actor failed with `failed to load /pause: permission denied`
