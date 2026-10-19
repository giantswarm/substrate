---
id: resources-schema-url-every-semconv-import-follow
fork: substrate
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: none
  refs: []
  exit: trivial upstream PR
depends_on: []
commit: 04c3342f
verified: 2026-10-19
---
# The resource's schema URL and every `semconv` import follow the semantic conventions of the SDK's …

- **Status:** carried
- **Upstream:** not filed; upstream `main` carries the same v1.40.0 imports since its SDK bump to v1.45.0
- **Our commit:** `04c3342f`; also [#129](https://github.com/giantswarm/substrate/pull/129) (`fix(otel): use the semantic conventions of the SDK's own detectors`)

## Change

The resource's schema URL and every `semconv` import follow the semantic conventions of the SDK's own detectors (v1.43.0 for `go.opentelemetry.io/otel/sdk` v1.45.0); `TestNewResourceDefaults` asserts the resource keeps the SDK's schema URL

## Reason

ate-api, ate-controller and atelet logged `partial telemetry resource: conflicting Schema URL .../1.40.0 and .../1.43.0` at every start and exported a resource without a schema URL; `newResource` tolerates `ErrSchemaURLConflict`, so no test saw it
