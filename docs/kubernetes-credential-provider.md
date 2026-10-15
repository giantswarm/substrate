# Kubernetes credential provider

The `k8s-credential-provider` Deployment follows the provider from
[upstream](https://github.com/agent-substrate/substrate/pull/1335). It serves
`CredentialProvider.FetchSecret` at `k8s-credential-provider.ate-system.svc:50051`
with its own ServiceAccount and projected serving certificate. AGW calls it
directly over mTLS to inject credentials into HTTP and intercepted HTTPS requests.

`ate-secret://k8s.io/default/team-a-secrets/example-api/token` resolves the `token`
entry in that Kubernetes Secret. The `default` locator and an explicit key are required.
The provider reads Kubernetes on every fetch and never persists or logs values.
AGW caches successful credentials per actor and URI for five minutes, so rotation
can take that long to reach injected requests.

Each request requires a trusted injector certificate with the configured SPIFFE
identity, an explicit atespace-to-namespace grant for the attested actor, and
Kubernetes `get` permission for the provider's ServiceAccount. Both installers
include the upstream get-only Secret ClusterRole and bind it to that ServiceAccount.
The provider can read Secrets across namespaces; its namespace policy controls
which namespaces each actor may use. Empty policies deny all requests.

## Configure the provider

AGW must support protocol-specific egress rules and `replace_headers`, along with
the [actor identity update](https://github.com/agentgateway/agentgateway/pull/3677)
and the current [FetchSecret contract](../pkg/proto/credproviderpb/credprovider.proto).

Create the MITM CA Secret using the existing installation tooling:

```sh
hack/install-ate-kind.sh --create-egress-mitm-ca-pool-secret
```

The gateway needs `egress-mitm-ca-pool` with `tls.crt` and `tls.key` in its namespace.
Actors making HTTPS requests must trust this CA; see the
[MITM trust bundle guide](egress-trust-bundle.md).

For Helm, add these values to your release configuration:

```yaml
credentialProvider:
  namespacePolicies:
  - atespace: team-a
    allowedNamespaces: [team-a-secrets]
```

A policy can select namespaces by label instead of listing them. With an
`ownNamespaceSelector` (a Kubernetes label selector, `matchLabels` and
`matchExpressions`) in place of `atespace` and `allowedNamespaces`, each
namespace the selector matches may be read by the atespace of the same name,
and by no other atespace:

```yaml
credentialProvider:
  namespacePolicies:
  - ownNamespaceSelector:
      matchExpressions:
      - {key: example.com/tenant, operator: Exists}
```

The provider watches namespaces and matches each request against the
namespace's labels at that moment, so a namespace created, labeled or unlabeled
after startup is granted or refused without a restart; while the provider
cannot watch namespaces (an API server outage, RBAC removed) it keeps the
labels it saw last. The reserved `ate-golden` atespace is never granted a
namespace this way. A `secretSelector` on
the policy narrows it as it narrows a listed grant. The selector must name at
least one requirement.

An atespace that serves the actors of many namespaces opts in to them with an
`allowedNamespaceSelector` beside `atespace` (and any `allowedNamespaces`): it
may read every namespace the selector matches, matched the same way. This is
how the golden actors, which every template boots in `ate-golden`, read the
credentials of their templates in tenant namespaces:

```yaml
credentialProvider:
  namespacePolicies:
  - atespace: ate-golden
    allowedNamespaces: [kagent]
    allowedNamespaceSelector:
      matchExpressions:
      - {key: example.com/tenant, operator: Exists}
```

The provider sees only the atespace `ate-golden` and the golden actor's name,
not the template it boots, so this grant lets every golden actor read every
matched namespace. A golden actor fetches only the credentials its template's
egress policy names, and kagent writes that policy when it translates the
template: it resolves the template's model, MCP servers, skills and shared
templates in the template's own namespace, so every credential the policy
names is a Secret of that namespace. kagent's translation of the template is
therefore the guard against a cross-tenant read by a golden actor; any other
writer of templates or egress policies in `ate-golden` must keep the same
rule.

The chart adds list/watch RBAC on namespaces only when a policy has an
`ownNamespaceSelector` or an `allowedNamespaceSelector`.

The Helm chart always deploys the provider and configures AGW's HTTP route and
HTTPS interception route. Namespace grants default to an empty list.
Policy changes roll the provider's Pods. Resource names and the injector identity
follow the release: release `demo` in namespace `platform` uses ServiceAccount
`demo-k8s-credential-provider`, endpoint
`demo-k8s-credential-provider.platform.svc:50051`, and injector identity
`spiffe://cluster.local/ns/platform/sa/demo-atenet-egress`.

HTTPS uses a dynamic backend: AGW selects the destination from the request and
validates its certificate using the system CA roots (`backendTLS: {}`). Public
APIs such as OpenAI and Anthropic need no per-backend certificates. The single
MITM CA lets AGW generate actor-facing certificates as needed. HTTP also travels
through the authenticated CONNECT tunnel, then leaves AGW over plaintext HTTP.

For the manifest installer, set your grants in
`manifests/egress-credential-injection/namespace-policy.yaml`, then deploy:

```sh
kubectl kustomize manifests/egress-credential-injection | ko apply -f -
hack/install-ate.sh --deploy-atenet \
  --atenet-dataplane=agentgateway --credential-provider='{"enabled":false}'
```

The provider is deployed separately above, so the installer's provider selection
is disabled. The fork's agentgateway manifest already points at that provider.
Set grants under `data.namespace-policy.yaml` in the policy ConfigMap, using
`policies:` with the same list as the Helm values. After applying policy changes,
restart the provider with `kubectl rollout restart -n ate-system
deployment/k8s-credential-provider`. Helm policy changes trigger a rollout
automatically. Client CA bundles and serving certificates reload automatically
for new TLS connections.

## Configure injection

Create the Secret and set `effects.replace_headers` on an actor's HTTP or HTTPS
egress rule to use credential URI
`ate-secret://k8s.io/default/team-a-secrets/example-api/token`, header
`authorization`, and prefix `Bearer `. Namespace grants alone do not create an
egress policy. The actor must send an `Authorization` header with a placeholder
value; AGW replaces it with the credential. Requests without that header are
forwarded unchanged. No ext_proc injector is needed.

## Tests

The Helm PR workflow installs the provider and MITM gateway from the start and
runs `internal/e2e/suites/credentials` alongside the standard suites with real actors,
Secrets, chart-managed RBAC, AGW, and the deployed provider. It checks
the exact injected token, unauthenticated and missing-placeholder controls, namespace-policy
denial and cache isolation between atespaces. The local origin serves HTTP;
the suite uses the installed gateway configuration without modifying ConfigMaps.

Include `-f internal/e2e/suites/credentials/values.yaml` in the initial Helm
installation to grant the test atespace access. After deploying the standard
MITM egress fixtures, run the suites together:

```sh
E2E_ATENET_DATAPLANE=agentgateway E2E_CREDENTIAL_PROVIDER=1 \
  hack/run-e2e-kind.sh -v -args --no-color
```

The credential suite tests HTTP injection. The existing MITM suite checks HTTPS
interception and actor trust against a public HTTPS origin using the same install.
