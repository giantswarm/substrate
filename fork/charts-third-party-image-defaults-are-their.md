# The chart's third-party image defaults are their `gsoci.azurecr.io/giantswarm/…` copies

- **Status:** carried
- **Upstream:** **ours to keep**: a Giant Swarm registry is not upstream's default; the upstream-shaped change is the row above (`upstream/qualified-image-refs`)
- **Our commit:** [#45](https://github.com/giantswarm/substrate/pull/45)

## Change

The chart's third-party image defaults are their `gsoci.azurecr.io/giantswarm/…` copies — `images.postgres` (`postgres:18.4-alpine@sha256:9a8afca5…`, the digest upstream pins as Docker Hub's floating `18-alpine`, under the versioned tag the mirror publishes it as), `images.rustfs` (`rustfs:1.0.0-beta.3@sha256:378642b0…`), `images.busybox` (`busybox:1.36`, rendered by no template), and `images.agentgateway` = `AGENTGATEWAY_IMAGE` (the agentgateway line's own release on gsoci, `agentgateway-upstream/agentgateway:2.0.0`, `sha256:63deaa67…`; first the mirror `agentgateway:v1.5.1-gs.4`, `sha256:f3d4b52c…`, of the line's release tag — the same upstream build) — tags and digests unchanged, the same bits. `images.awsCli` stays upstream's short name (the row above: the bucket-init Job's pod template is immutable); its copy `gsoci.azurecr.io/giantswarm/aws-cli:2.17.0` exists for the release that lets the Job be recreated. `charts/substrate/tests/images_test.yaml` pins the rendered images

## Reason

every Giant Swarm installation pulls from gsoci.azurecr.io and its image policy trusts the Giant Swarm signing identity (giantswarm/agent-platform#575, #580): a chart default off gsoci is pulled from a public registry the fleet does not otherwise reach and no policy admits. Until this release the meta chart forwards the same references in its `substrate.images` block (giantswarm/agent-platform, values.yaml), which becomes redundant with it
