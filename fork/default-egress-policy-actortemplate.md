---
id: default-egress-policy-actortemplate
fork: substrate
theme: egress-policies
category: feature
status: carried
upstream:
  outcome: superseded
  refs: [agent-substrate/substrate#1324, agent-substrate/substrate#1558, agent-substrate/substrate#1868, agent-substrate/substrate#2159, kagent-dev/kagent#2604]
  exit: re-pin onto as#2159's shape; field 9 changes meaning
depends_on: [giantswarm/kagent-upstream:substrate-the-compiled-actortemplate-carries-the-revisions-egress]
commit: e1e2d822
verified: 2026-10-19
---
# A default egress policy on the ActorTemplate

- **Status:** carried
- **Upstream:** design B of [agent-substrate/substrate#1324](https://github.com/agent-substrate/substrate/issues/1324); our draft agent-substrate/substrate#1868 was closed on 2026-09-29 in favour of the maintainer's [agent-substrate/substrate#1558](https://github.com/agent-substrate/substrate/pull/1558), which added the field; #1558 was closed unmerged on 2026-10-06 in favour of [agent-substrate/substrate#2159](https://github.com/agent-substrate/substrate/pull/2159) (`ActorTemplate.golden_egress_policy`, golden actor only, same field number 9, open), which gives the golden actor its policy; ordinary actors get theirs from the consumer (`CreateActorEgressPolicy`, kagent already does), so the template default only matters for the golden actor. Exit: when #2159 merges, consumers set `golden_egress_policy` (omitted when the rules are empty, #2159 rejects an empty list) and the line drops this patch at the next re-pin. The kagent line sets the field from the revision's egress destinations and credentials (kagent-upstream carried patch, [kagent-dev/kagent#2604](https://github.com/kagent-dev/kagent/issues/2604)). At the 2026-10-08 re-pin onto v0.4.0-alpha1: upstream moved request validation into `cmd/ateapi/internal/apivalidation`; `ValidateCustom_EgressPolicyTemplate_Rules` and the template case of the rules test live there now.
- **Our commit:** `e1e2d822`, `1880b995`, `7f05a6c7` (the `git cherry-pick -x` of agent-substrate/substrate#1558's `c7de422d`, `7ae519bd`, `6c7e6b97`, which replaced the line's own `c87cc0d5` of [#56](https://github.com/giantswarm/substrate/pull/56) at the 2026-10-05 re-pin; their tests on upstream's GA egress rules)

## Change

A default egress policy on the ActorTemplate (`default_egress_policy`, an `EgressPolicyTemplate` = the rules of an `EgressPolicy`), copied onto every actor created from the template as its `default` policy in the same transaction as the actor row

## Reason

atenet denies every request of an actor that has no `EgressPolicy` (`errNoPolicy`, HTTP 403). kagent creates the policy only in the AgentInstance workflow; the golden actor the template reconciler creates gets none, so a golden boot that fetches git skills before readyz fails with 403 and the template never gets its golden snapshot (`agentlab skills-test` on the v0.2.0-beta4 pin)
