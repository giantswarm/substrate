---
id: route-outcome-denied-for-a-403
fork: substrate
theme: credential-injection
category: bugfix
status: carried
commits:
  - 'fix(atenet): classify a 403 route outcome as denied, not resume_error'
upstream:
  outcome: merged
  refs: [agent-substrate/substrate#2534]
  exit: "drops at the re-pin onto a commit containing 4ebd668d6 (as#2534)"
depends_on: []
verified: 2026-10-26
---
# The router's route outcome names a 403 denied, not resume_error

## Change

`classifyOutcome` (`cmd/atenet/internal/router/extproc/metrics.go`) records `ate.router.outcome=denied` on `atenet.router.route.duration` for a 403 `ReqError` and a `PermissionDenied` status; `resume_error` stays for a resume that failed for any other reason. `internal/ateattr` (`RouterOutcomeDenied`), the registry in `docs/metrics/registry/metrics.yaml` and the outcome list in `docs/observability.md` name the value. Unit tests cover the 403 and `PermissionDenied` cases and the `resume_error` cases that remain.

## Reason

Without a 403 case every egress policy refusal and every ingress `PermissionDenied` counted as `resume_error`, the router-defect outcome, so an alert on router defects fired on ordinary policy decisions and a real resume failure hid among them. Why an egress request was refused is on `atenet.router.egress.refused`.

## History

[agent-substrate/substrate#2534](https://github.com/agent-substrate/substrate/pull/2534), open, from the branch `upstream/egress-denied-outcome` on upstream `main` @ `579cfcb6`

[#354](https://github.com/giantswarm/substrate/pull/354): the fork's cherry-pick of the upstream commit, unchanged

2026-10-20, decided on internal issue 38652, comment 6301541135: class 5 upstream PR in flight; destination agent-substrate/substrate#2534; first step fix the failing e2e check on it; branch `upstream/egress-denied-outcome` @ `f7a635bd1`; test the e2e case of #2534; depends on no theme; owner unassigned; draft: internal issue 38652, comment 6301546241.

2026-10-26: agent-substrate/substrate#2534 merged on 2026-10-23 as `4ebd668d6`, so the outcome moves to merged.
