---
id: e2e-credential-fixture-adds-grants
fork: substrate
theme: test-flakes
category: test
status: carried
commits:
  - 'fix(e2e): add the credinject grants to the provider policy, not swap it'
upstream:
  outcome: to-file
  refs: []
  exit: kd PR
depends_on: []
verified: 2026-10-19
---
# The e2e credential fixture adds its grants to the provider policy instead of swapping it

## Change

`e2e.ConfigureCredentialProvider` (`internal/e2e/credprovider.go`) reads the installed `ate-system/k8s-credential-provider-namespace-policy`, appends a grant of `ate-e2e-credinject-secrets` for each egresscredinject probe atespace (once, keeping every existing grant and field), and on a passing test puts the policy back exactly as it found it, where it applied a policy of its own and restored the kustomize install's shipped one. `internal/e2e/fixtures/credinject/credinject.yaml` keeps the Namespace and the Secret. Unit tests cover the merge.

## Reason

The e2e suite packages run in parallel against one install, and the helm-e2e lane installs the chart with `suites/credentials`' grants. The swap and its cleanup revoked those grants while `TestKubernetesCredentialInjection` still resolved its Secrets, which failed the lane with `403 ... is not permitted to resolve secrets` on changes that never touched the provider.

## History

to file — prepared in the fork: branch [`upstream/e2e-credprovider-additive`](https://github.com/giantswarm/substrate/tree/upstream/e2e-credprovider-additive) on the mirror `main` @ `5cf1c245` (kagent-dev/substrate, where `suites/credentials` and the helm-e2e lane come from; agent-substrate/substrate has neither)

[#359](https://github.com/giantswarm/substrate/pull/359): the same commit as the prepared branch
