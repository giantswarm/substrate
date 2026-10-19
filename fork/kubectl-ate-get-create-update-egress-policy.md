---
id: kubectl-ate-get-create-update-egress-policy
fork: substrate
theme: egress-policies
category: feature
status: dropped
commits: []
upstream:
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#1659, agent-substrate/substrate#1813]
  exit: carried on release-1.3 only; no commit on giantswarm, dropped from this ledger once release-1.3 is retired
depends_on: []
verified: 2026-10-19
---
# `kubectl ate get`, `create` and `update egress-policy` on `release-1.3`

## Change

`kubectl ate get`, `create` and `update egress-policy` on `release-1.3` (`cmd/kubectl-ate/internal/cmd/egress_policy.go` and the printer's `egress_policy.go`: upstream's commands with their unit tests and README section, written in that branch's `EgressRule` shape, `hostnames`, `cidrs`, `all`; `gopkg.in/yaml.v3` becomes a direct dependency, as upstream's; the networking e2e suite's `TestKubectlAteEgressPolicy` drives the branch's binary through create, get and update of one policy and checks the stored rules over gRPC)

## Reason

the branch's client had no egress-policy command, so an operator of a 1.3 cluster reached for the line's `kubectl-ate`, whose `EgressRule` is `http`, `https`, `tls_passthrough` on the same field numbers 1–3: a 1.3 ate-api accepts such a policy and stores it with another meaning, a `tls_passthrough` rule as `all` (every destination allowed), an `https` rule as `cidrs`, with no error on either side ([giantswarm/substrate#146](https://github.com/giantswarm/substrate/issues/146), part 1; part 2 is ate-api reporting its contract version on both lines and the client refusing a server of another contract)

## History

upstream's own commits `514e6109` (agent-substrate/substrate#1659) and `d7d70420` (#1813), in every upstream release since v0.2.0; a backport for the 1.3 line alone, which ends with the branch

`giantswarm`: the pin, v0.3.0-alpha3 carries both upstream commits; `release-1.3`: [#156](https://github.com/giantswarm/substrate/pull/156)
