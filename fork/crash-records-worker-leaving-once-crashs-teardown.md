# A crash records its worker leaving

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2480](https://github.com/agent-substrate/substrate/pull/2480), from the branch `upstream/resume-restore-cut-by-drain`, stacked on [agent-substrate/substrate#2479](https://github.com/agent-substrate/substrate/pull/2479): upstream's crash frees the worker without a teardown, so the worker is read right after its release
- **Our commit:** [#306](https://github.com/giantswarm/substrate/pull/306): the fork's own commit

## Change

A crash records its worker leaving: once the crash's teardown is done, `crashActor` reads the worker the actor was assigned, and one that is draining, whose record is gone or whose pod was replaced is named in the crash (`assigned worker is draining`, `assigned worker no longer exists`, `worker pod went away while hosting the actor`), causes the resume retry's and the golden take retry's list already holds; a failure on a worker that keeps serving stays terminal

## Reason

a resume whose worker pod was deleted mid-restore while atelet stayed reachable crashed for good: the sandbox was killed with the pod (`runsc start: signal: killed`), the teardown succeeded, and nothing in the crash named a cause a roll produces, so a worker drain during a Substrate step lost the conversation ([#305](https://github.com/giantswarm/substrate/issues/305)). Now the resume returns the actor to its snapshot and retries it on another worker
