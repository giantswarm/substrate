# Read ate-api-server's PostgreSQL connection string from a Secret

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#32](https://github.com/kagent-dev/substrate/pull/32), a draft since 2026-09-29, not ours; agent-substrate/substrate#1979 (open draft, 2026-09-29: PostgreSQL credentials from Secret files, a `@file:` DSN with reload) is the server-side successor, reviewed instead of a filing of ours — falls away at the re-pin onto the first release that carries either. At the 2026-10-08 re-pin onto v0.4.0-alpha1: upstream split the DSN into `postgres.readWriteConnectionString` and `postgres.ownerConnectionString` (separate roles for runtime and migrations); the Secret feeds `ATE_API_POSTGRES_READ_WRITE_CONNECTION_STRING` and, unless `postgres.ownerConnectionString` is set, `ATE_API_POSTGRES_OWNER_CONNECTION_STRING` from the same key, so the value name the meta chart sets is unchanged.
- **Our commit:** `b2623ebf` (`git cherry-pick -x 1872249e`) and `917660f1` (`git cherry-pick -x 41097da7`, the `helm plugin install --verify=false` of the same pull request), [#8](https://github.com/giantswarm/substrate/pull/8)

## Change

Read ate-api-server's PostgreSQL connection string from a Secret (`postgres.connectionStringSecretRef`; the `ate-api-server-envvars` ConfigMap then carries only the schema)

## Reason

meta chart 4.0 puts Substrate's control-plane database on the platform's CNPG cluster and hands ate-api-server the DSN through a Secret, never a ConfigMap ([giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342))
