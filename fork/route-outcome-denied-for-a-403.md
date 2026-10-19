---
id: route-outcome-denied-for-a-403
fork: substrate
theme: credential-injection
category: bugfix
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#2534]
  exit: as#2534 merged
depends_on: []
commit: giantswarm/substrate#354
verified: 2026-10-19
---
# The router's route outcome names a 403 denied, not resume_error

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2534](https://github.com/agent-substrate/substrate/pull/2534), open, from the branch `upstream/egress-denied-outcome` on upstream `main` @ `579cfcb6`
- **Our commit:** [#354](https://github.com/giantswarm/substrate/pull/354): the fork's cherry-pick of the upstream commit, unchanged

## Change

`classifyOutcome` (`cmd/atenet/internal/router/extproc/metrics.go`) records `ate.router.outcome=denied` on `atenet.router.route.duration` for a 403 `ReqError` and a `PermissionDenied` status; `resume_error` stays for a resume that failed for any other reason. `internal/ateattr` (`RouterOutcomeDenied`), the registry in `docs/metrics/registry/metrics.yaml` and the outcome list in `docs/observability.md` name the value. Unit tests cover the 403 and `PermissionDenied` cases and the `resume_error` cases that remain.

## Reason

Without a 403 case every egress policy refusal and every ingress `PermissionDenied` counted as `resume_error`, the router-defect outcome, so an alert on router defects fired on ordinary policy decisions and a real resume failure hid among them. Why an egress request was refused is on `atenet.router.egress.refused`.
