# substrate-crds

Helm chart for installing the Agent Substrate CRDs.

Install this chart before the `substrate` and `substrate-atelet` charts:

```bash
helm upgrade --install substrate-crds ./charts/substrate-crds
```

Uninstalling the release keeps the CRDs (`helm.sh/resource-policy: keep`), so
it deletes no WorkerPool, SandboxConfig or CSIDriverConfig. Delete the CRDs
with kubectl to remove them.

Then install the other charts as described in the
[substrate chart README](../substrate/README.md).

The CRD YAMLs in `templates/` mirror `manifests/ate-install/generated/`.
Run `hack/verify/crd-chart.sh` to verify they are in sync.
