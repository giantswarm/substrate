# The identity e2e suite rotates the egress trust bundle without dropping the gateway's CA

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/e2e-mitm-trust-rotation`](https://github.com/giantswarm/substrate/tree/upstream/e2e-mitm-trust-rotation) here (`580f51c1`, on kagent-dev/substrate `main` @ `08c930d6`, which moved past the mirror; DCO-signed). A team member opens the kagent-dev/substrate pull request once the team has reviewed it. kagent-dev/substrate `cf3eb024` (2026-09-25, in v0.3.0-alpha1) fixes the same flake differently: `.github/workflows/helm-e2e.yaml` runs every suite with `-skip '^TestActorIdentity_'` and the identity suite in a second step afterwards, so the rotation no longer overlaps the HTTPS egress tests. On alpha1 the flake is gone without this patch: it drops at the re-pin and nothing is filed (agent-substrate#1668, the rotated bundle after suspend/resume, is the nearest product-side item)
- **Our commit:** `fb99fa1a` ([#65](https://github.com/giantswarm/substrate/pull/65))

## Change

The identity e2e suite rotates the egress trust bundle without dropping the gateway's CA (`RotateEgressTrustPool` replaces `ReplaceEgressTrustPool`: it adds a CA to `egress-mitm-ca-pool` next to the ones already there, drops only one it added itself, leaves `tls.crt`/`tls.key` alone, and puts the pool back when the test ends; atelet shuffles a projected bundle's anchors, so the suite compares it as a set of certificates, `SameCertificates`)

## Reason

helm-e2e sets `E2E_EGRESS_MITM` for every suite and `go test` runs the packages in parallel. The identity suite's three single-CA replacements of the cluster-wide pool reached actors within seconds through the ClusterTrustBundle, but reached the gateway a minute or more later through its Secret volume. A networking-suite actor that loaded its anchors in between failed `TestActorEgressHTTPSByHostnameMITM`, and at times `TestActorEgressHTTPSByAddress`, with `x509: certificate signed by unknown authority` for the whole 30 s budget, on pull requests that do not touch the egress path. kagent-dev/substrate's own helm-e2e fails the same way. Test-only. The product gap it exposed, no overlap rotation for this CA, is covered by the CA-pool rotation change. Proven by three consecutive green helm-e2e runs on the pull request, with the MITM test starting during or seconds after the rotations
