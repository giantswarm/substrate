---
id: every-gvisor-workerpool-runs-releases-own-ateom
fork: substrate
theme: compat-migrations
category: gs-specific
status: carried
commits:
  - 'fix(atecontroller): name a WorkerPool''s worker image in its status'
  - 'fix(atecontroller): run gVisor workers on the release''s own ateom'
upstream:
  outcome: fork-only
  refs: [agent-substrate/substrate#1357]
  exit: 'agent-platform pins Substrate to one exact version and sets every pool''s spec.workerImage to it (tenant pools through agent-manager); then the commits drop at the next re-pin'
  class: 3
  destination: 'giantswarm/agent-platform Substrate version pin and WorkerPool spec.workerImage; giantswarm/agent-manager tenant pools'
  first_step: 'pin Substrate to one exact version and derive every pool''s spec.workerImage from it (giantswarm/agent-platform)'
  source: 'giantswarm @ b9ee9cece 96a403cd4'
  test: 'agent-platform verify-worker-image rewritten for the exact pin; lab check that worker and atelet versions match'
  recheck: 'the agent-platform release with the exact pin'
  owner: unassigned
  draft: ''
  decision: 'issuecomment-6301319662, amended issuecomment-6339634309'
  couplings: [compat-migrations]
  open_question: ''
depends_on: [actors-run-as-their-images-user-workdir]
verified: 2026-10-21
---
# Every gVisor WorkerPool runs the release's own `ateom-gvisor`

## Change

Every gVisor WorkerPool runs the release's own `ateom-gvisor` (atecontroller `--gvisor-worker-image` replaces a gVisor pool's `spec.workerImage` in the worker Deployment; the chart sets it to `ateom-gvisor` composed like the other component images, `ateController.gvisorWorker.followRelease`, on by default; micro-VM pools and the `spec.workerImage` field are unchanged; the pool's `status.workerImage` names the image its workers run, and an override is the condition `WorkerImageOverridden=True` (reason `GVisorWorkerImageFlag`, both images in its message) and a `Warning` event `WorkerImageOverridden` on the pool; helm-e2e builds `ateom-gvisor` and points the counter pool at `1.3.0` before the gVisor suites)

## Reason

atelet and ateom share a node-local protocol with no cross-version guarantee, but the platform set the pool's worker to the floor of its Substrate range while Flux moved atelet to the newest release in it: atelet `1.3.1-rc.4` (the `USER` row above) with worker `1.3.0` left the pause container's `/` at `0700`, and every gVisor actor failed with `failed to load /pause: permission denied`

## History

fork-only: upstream installs pin atelet and ateom to one build per node (agent-substrate/substrate#1357)

[#114](https://github.com/giantswarm/substrate/pull/114), [#309](https://github.com/giantswarm/substrate/pull/309)

2026-10-21: decided in the fork-reduction review of the compat-migrations theme: platform (class 3): upstream sets the worker image per pool (agent-substrate/substrate#1812 removed the version-pinned rollout this cited), so agent-platform pins one exact Substrate version and sets every pool's image to it.

2026-10-22: amended in the fork-reduction review of the compat-migrations theme: the conversion re-pin is the first past Substrate GA (fallback 2026-11-24), with one conversion written then against the frozen format; whether it runs as a one-shot agent-platform Job or as fork code for that one re-pin is decided then.
