---
id: podcertcontroller-runs-postgres-signer-only-with-ca-pool
fork: substrate
theme: chart-shape
category: gs-specific
status: carried
commits:
  - 'fix(podcertcontroller): run the PostgreSQL signer only with a CA pool'
upstream:
  outcome: fork-only
  refs: []
  exit: chart-shape migration
depends_on: [chart-keeps-lines-shape-on-v0-5-0-alpha2-base]
verified: 2026-10-19
---
# The certificate controller runs the PostgreSQL signer only with a CA pool

## Change

`podcertcontroller` starts the `postgres.podcert.ate.dev` signer only when `--postgres-ca-pool` names a file; without it, the controller signs service DNS and pod identity certificates alone and logs that it signs no PostgreSQL login certificates.

## Reason

v0.5.0-alpha2 loads a third CA pool unconditionally and exits without it. The line's chart keeps its PostgreSQL interface (ate-api-server logs in with its pod identity certificate), mounts two pools, and has no PostgreSQL pool to give: the controller crashed, and no pod of the release received a certificate. An installation upgrading from 1.8 has no such pool either, so requiring one would be a manual step.

## History

none: the line's PostgreSQL interface, which upstream replaced with per-role login certificates

the re-pin onto v0.5.0-alpha2 (`fix(podcertcontroller): run the PostgreSQL signer only with a CA pool`)
