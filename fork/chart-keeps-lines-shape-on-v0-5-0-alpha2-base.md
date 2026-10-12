# The chart keeps the line's shape on the v0.5.0-alpha2 base

- **Status:** carried
- **Upstream:** none: the line's chart interface, which its consumers install
- **Our commit:** the re-pin onto v0.5.0-alpha2 (`fix(chart): keep the line's chart shape on the v0.5.0-alpha2 base`)

## Change

The substrate chart keeps the line's PostgreSQL interface (the bundled StatefulSet, `postgres.connectionStringSecretRef`, schema `public`, the roles through `@env`) and installs the pod certificate controller itself. Upstream's chart changes that touch neither are taken: per-component `resources`, the rustfs ServiceAccount, the controller Role in the release namespace, `atelet.imageCredentialProviderConfigYAML`, and the separate `substrate-podcert` chart. The egress gateway keeps `atenetEgress.resources` (ephemeral storage only) instead of upstream's `atenetEgress.agentgateway.resources`, and atenet-router keeps the line's routes with request parking. Upstream's chart tests that assert its own shape follow the line's: the application chart installs the controller, there is no `postgres.clientCertificates`, and atelet renders its pinned-images ConfigMap beside the credential provider's.

## Reason

v0.5.0-alpha2 moves PostgreSQL out of the chart (pre-created Secrets per role, client certificates, schema `substrate`) and the certificate controller into a chart of its own. Taken as is, an upgrade from a 1.8 release would lose the database the chart installed and the trust chain of every pod; the line's shape upgrades an installation in place. Upstream's new default limits (atelet 1Gi, ate-api 1Gi, ate-controller 512Mi memory) apply on the upgrade.
