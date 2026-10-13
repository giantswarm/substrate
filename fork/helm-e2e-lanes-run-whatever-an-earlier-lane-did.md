# helm-e2e's lanes each run, whatever an earlier lane did

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#71](https://github.com/kagent-dev/substrate/pull/71); agent-substrate/substrate has no helm-e2e. Falls away at the re-pin onto the first release that carries it
- **Our commit:** [#366](https://github.com/giantswarm/substrate/pull/366)

## Change

Every `Run E2E tests (…)` step of helm-e2e runs once the setup before it succeeded (the `lanes-ready` step), unless the run is cancelled, and reports its own outcome. The job and its name (`e2e-test`) are unchanged.

## Reason

The lanes are steps of one job, so a failed lane skipped every later one: a flake in the gVisor lane hid the micro-VM lane on the same head until a re-run ([#365](https://github.com/giantswarm/substrate/issues/365)). Workflow-only.
