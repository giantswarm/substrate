# The line publishes the substrate-podcert chart beside substrate-crds and substrate

- **Status:** carried
- **Upstream:** fork-only, never upstream: upstream publishes its own charts to its own registry from its own pipeline
- **Our commit:** `ci(fork): publish the substrate-podcert chart` ([#360](https://github.com/giantswarm/substrate/issues/360))

## Change

The `charts` job of `.circleci/config.yml` stamps `charts/substrate-podcert/values.yaml` the way it stamps `charts/substrate/values.yaml`, setting `image.registry`, `image.repository` and `image.tag` to this registry and the artifact version. It then packages the chart with `version` = `appVersion` = the artifact version, pushes it to `oci://gsoci.azurecr.io/giantswarm/substrate/helm`, and signs it with the other two charts.

## Reason

Upstream v0.5.0-alpha2 split the pod certificate controller into a chart of its own, `charts/substrate-podcert`. Upstream kagent's `kagent install` installs that chart from the Substrate chart registry at the pinned Substrate version, before `substrate`. The line kept the controller inside its `substrate` chart (`chart-keeps-lines-shape-on-v0-5-0-alpha2-base.md`) but never published the split chart, so a kagent pinned to a line release failed on `failed to resolve gsoci.azurecr.io/giantswarm/substrate/helm/substrate-podcert:<version>: not found`. gsoci.azurecr.io is the line's only registry, so the chart is published there.
