---
id: ate-api-reports-egress-policy-contract-kubectl
fork: substrate
theme: egress-policies
category: gs-specific
status: carried
commits:
  - 'fix(ateapi): name the egress-policy contract after v0.4.0-alpha1'
  - 'feat(ateapi): report the egress-policy contract, kubectl-ate checks it'
upstream:
  outcome: fork-only
  refs: []
  exit: drops when path_prefix lands upstream or is dropped
depends_on: [egress-scopes-credential-header-to-request-path-prefix]
verified: 2026-10-19
---
# ate-api reports its egress-policy contract and `kubectl ate create` and `update egress-policy` …

## Change

ate-api reports its egress-policy contract and `kubectl ate create` and `update egress-policy` check it (`rpc GetEgressPolicyContract` returning `EgressPolicyContract{version}`, the version `ateapipb.EgressPolicyContractVersion`: `v0.3.0-alpha3` on `giantswarm`, `v0.2.0-beta5` on `release-1.3`; the client refuses a server that reports another version, naming both, before it writes; against a server that answers `Unimplemented` it refuses a rule without hostnames (an empty `tls_passthrough` is a valid `all` on 1.3 and reads back unchanged), writes, reads the stored policy back and refuses with the difference when its rules are not the ones the server accepted, deleting nothing; unit tests for both paths and the README section)

## Reason

the two lines' `EgressRule` give field numbers 1–3 another meaning (`http`, `https`, `tls_passthrough` against `hostnames`, `cidrs`, `all`), so a client of one line writing to a server of the other stored a policy with another meaning and no error on either side, a `tls_passthrough` rule as `all` ([giantswarm/substrate#146](https://github.com/giantswarm/substrate/issues/146), part 2). A server stores a policy without the fields it could not decode but echoes them in its answer, so the read-back shows what it enforces

## History

none upstream yet: written for upstream (an `EgressRule` change across upstream's alphas meets the same mismatch); the next re-pin onto a release whose `EgressRule` differs bumps the version. At the 2026-10-08 re-pin onto v0.4.0-alpha1 the contract is `v0.4.0-alpha1`: `CredentialHeader` gained `actor_jwt` (field 4), so a kubectl-ate of the alpha3 contract refuses a 1.6 server and the other way round.

`giantswarm`: [#160](https://github.com/giantswarm/substrate/pull/160); `release-1.3`: [#161](https://github.com/giantswarm/substrate/pull/161)
