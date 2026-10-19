---
id: ate-idp-server-serves-actor-jwt-issuer
fork: substrate
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: drops-at-repin
  refs: [agent-substrate/substrate#2099]
  exit: re-pin onto kd v0.6.0-alpha3 or later; prepared chart branch superseded
depends_on: []
commit: 08779062
verified: 2026-10-19
---
# ate-idp-server serves the actor-JWT issuer

- **Status:** carried
- **Upstream:** the server is upstream's `08779062` ([agent-substrate/substrate#2099](https://github.com/agent-substrate/substrate/pull/2099), merged 2026-10-16, in kagent-dev/substrate v0.6.0-alpha3, not in v0.5.0-alpha4) and leaves at the first re-pin that contains it; the chart's Deployment and `idp` Service: kagent-dev/substrate `main` renders `charts/substrate/templates/ate-idp-server.yaml` since `8cdb6963` (2026-10-18, in v0.6.0-alpha3), so the branch prepared in the fork, `upstream/helm-ate-idp-server`, is superseded and the chart half leaves at the same re-pin
- **Our commit:** [#472](https://github.com/giantswarm/substrate/pull/472): `git cherry-pick -x` of `08779062` (the module path adapted, only its ate-idp-server entry of `docs/metrics/substrate.yaml`), the chart's `ate-idp-server` and the line's image of it

## Change

`cmd/ate-idp-server` serves `/.well-known/openid-configuration` and `/openid/v1/jwks` over HTTPS for the issuer ate-api-server places in every actor JWT, built per request from the actor-JWT authority pool ate-api-server signs with (`--actor-id-jwt-pool`, reread at most once a minute). The chart renders it in the release namespace: a Deployment mounting the `actor-id-jwt-pool` Secret read-only without subPath and a servicedns pod certificate, its ServiceAccount and PodDisruptionBudget, and the Service `idp` (port 443 to its listener), whose name the certificate carries. ate-api-server's `--actor-jwt-issuer` and ate-idp-server's `--issuer` come from one helper, `ateApi.actorJWTIssuer` or `https://<idp Service>.<namespace>.svc`. The line's CircleCI builds, pushes and scans `gsoci.azurecr.io/giantswarm/substrate/ate-idp-server` with the other components.

## Reason

Without it nothing published the issuer's key set, so no relying party (an MCP gateway that trusts actor JWTs, for one) could verify an actor JWT on any installation of the line ([#471](https://github.com/giantswarm/substrate/issues/471)).
