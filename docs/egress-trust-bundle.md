# Enabling man-in-the-middle (MITM) interception for Actor Egress policy

## Overview

Substrate has special handling for traffic leaving actors --- it is routed
through the egress gateway.  Connections that use TLS (likely the vast majority
of your traffic to external destinations, like requests to google.com) require
special configuration inside your actor in order to successfully establish the
TLS connection.

The exact needed configuration is slightly different depending on whether you
are using `tls_passthrough` (no interception) rules, or `https` (interception)
rules.  This doc gives you a recipe for configuring your actors that will work
for both scenarios.

Connections allowed via a `tls_passthrough` rule are just forwarded through the
egress gateway, with no modifications.  The TLS connections are established
directly between the actor and the external destination, so the egress gateway
has no visibility into the connection (beyond metadata such as the destination).

Connections allowed via an `https` rule have TLS terminated at the egress
gateway, so that the gateway can inspect the requests and inject credentials.
The gateway then open a new TLS connection to the real destination, and sends
the modified requests.

The egress gateway does the TLS termination using server certificates it mints
on-the-fly using a built-in CA.  In order for the interception to work, the code
you run inside the actor must be explicitly configured to trust this built-in
CA.  Substrate can automatically inject the root certificates for this CA into
the actor's filesystem using a SystemInfo volume with a TrustBundle data source.

Note: DNS traffic from the actor (both TCP and UDP) is not forwarded to the
gateway, and thus cannot be affected by egress policies.  Instead, the DNS
traffic is allowed to directly exit the worker pod and be answered by the host
cluster's configured DNS server.

## Configuring a workload to trust public and man-in-the-middle CAs

Add a `systemInfo` volume with a `trustBundle` data source, and mount it:

```yaml
apiVersion: ate.dev/v1alpha1
kind: ActorTemplate
metadata:
  name: my-actor
  namespace: my-namespace
spec:
  volumes:
  - name: system-info
    systemInfo:
      dataSources:
      # This file will contain trust anchors for both the MITM CA and standard
      # public WebPKI CAs, meaning it will work for actors that use `https` rules,
      # `tls_passthrough` rules, or a mix.
      - trustBundle:
          names:
          - egress-mitm.ate.dev
          - system-roots.ate.dev
          path: trust-bundle.pem
  containers:
  - name: app
    image: ...
    volumeMounts:
    - name: system-info
      mountPath: /run/ate/egress-trust-bundle/
```

Substrate only offers a few built-in trust bundles with well-known names:
* `egress-mitm.ate.dev` — the egress gateway CA.
* `system-roots.ate.dev` — A selection of public CA root certificates provided
  by Substrate.  In official Substrate images, this is the Mozilla Root Store as
  shipped on Debian (consumed via the distroless-static base image).

You then need to configure your application to use the trust-bundle file.  In
general, this is application specific, but many runtimes respect the
SSL_CERT_FILE environment variables.  Here are some common scenarios and the
configuration they support:

| Runtime/Library | Setting | Notes |
|---|---|---|
| Go | SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem |  |
| Python (most things) | SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem | Anything using OpenSSL's default paths: `ssl`, `urllib`, `aiohttp`, `psql` ... |
| Python (requests) | REQUESTS_CA_BUNDLE=/run/ate/egress-trust-bundle/trust-bundle.pem | |
| pip (uses requests) | REQUESTS_CA_BUNDLE=/run/ate/egress-trust-bundle/trust-bundle.pem | |
| Node.js | NODE_EXTRA_CA_CERTS=/run/ate/egress-trust-bundle/trust-bundle.pem | |
| Deno | DENO_CERT=/run/ate/egress-trust-bundle/trust-bundle.pem | |
| curl | SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem ||
| git (HTTPS remotes) | GIT_SSL_CAINFO=/run/ate/egress-trust-bundle/trust-bundle.pem ||

Note: If your application relies on an additional private CA built into the
system trust store of your base container image (for example, if your enterprise
setup has an additional layer of TLS man-in-the-middle for traffic leaving your
network), then you will find that whether or not it is respected depends on the
software you are running inside your actor.  For example, Deno and Python
requests treat their environment variable overrides as *completely replacing*
the system trust store, where as most things that support SSL_CERT_FILE treat it
as *additive* to the system trust store.  If you need to support this scenario,
you will need to use an entrypoint wrapper to build a file containing the
substrate-provided roots along with your custom roots (or directly configure
this setup in your application's startup logic).    See the [example](#example-entrypoint-wrapper-for-custom-roots) below.

## Example entrypoint wrapper for custom roots

```sh
#!/bin/sh
set -eu
sys=/etc/ssl/certs/ca-certificates.crt   # RHEL family: /etc/pki/tls/certs/ca-bundle.crt
ca=/run/ate/trust-bundle.pem
out=/tmp/ca-bundle.pem
{ cat "$sys"; echo; cat "$ca"; } > "$out"
export SSL_CERT_FILE="$out" REQUESTS_CA_BUNDLE="$out" CURL_CA_BUNDLE="$out" GIT_SSL_CAINFO="$out" PIP_CERT="$out"
export NODE_EXTRA_CA_CERTS="$ca" DENO_CERT="$ca"
exec "$@"
```

Note: Do not bake the bundle into the image at build time. It will tie the image to one cluster's CA and breaks on rotation.

## Verify

`demos/egress/egress-template.yaml.tmpl` is a complete working template that
does exactly this. Deploy it:

```bash
./hack/install-ate.sh --deploy-demo-egress
```

Then drive an actor's egress at an HTTPS URL an `https` rule allows and
confirm it returns a response rather than a certificate error. The minted leaf
chains to no public root, so a `200` is positive evidence that the projected
bundle did the validating.

## Operational notes

**A bundle that does not resolve fails the actor start.** If the name is not on
the allowlist, the backing ClusterTrustBundle is missing, or the bundle is empty
or unparseable, the actor does not start — an actor that declared a trust bundle
must not run without one.

atelet logs it on the node that was going to host the actor, as the `err` field
of the interceptor's `Handle RPC` record, at INFO, with
`method=/atelet.AteomHerder/Run` (or `/atelet.AteomHerder/Restore` when a
suspended actor is coming back):

```
while populating system-info volume "system-info": system-info projection "trust-bundle.pem": trust bundle "egress-mitm.ate.dev": ClusterTrustBundle "egress-mitm.ate.dev:mitm:primary-bundle" not found
```

ateapi surfaces the same text to the caller that asked for the actor, wrapped
once by the resume step and once by gRPC:

```
while creating workload from spec: rpc error: code = Internal desc = while populating system-info volume "system-info": system-info projection "trust-bundle.pem": trust bundle "egress-mitm.ate.dev": ClusterTrustBundle "egress-mitm.ate.dev:mitm:primary-bundle" not found
```

That is the common case: projecting `egress-mitm.ate.dev` on an install where
nothing has created the `egress-mitm-ca-pool` Secret the bundle derives from. `system-info` is the volume's `name` from your
template and `trust-bundle.pem` its `path`, so those two vary with what you
wrote. The other failure modes differ only in the innermost clause:

| Cause | Innermost clause |
|---|---|
| Name not on the allowlist | `trust bundle "my-own-bundle" is not supported by this deployment (supported: egress-mitm.ate.dev)` |
| Bundle present but empty or unparseable | `trust bundle "egress-mitm.ate.dev": unusable ClusterTrustBundle "egress-mitm.ate.dev:mitm:primary-bundle": …` |

**A certificate error is a trust-store problem, not a gateway one.** Substrate
cannot see how the actor loaded the bundle, so the first signal is the
application's own error: `x509: certificate signed by unknown authority` from
Go, `CERTIFICATE_VERIFY_FAILED` from Python, exit code 60 from curl.

| Symptom | Likely cause |
|---|---|
| Fails on an `https`-rule host, `tls_passthrough` hosts work | The gateway CA is missing: projection absent, wrong path, or a variable the runtime does not read |
| Fails on a `tls_passthrough` host, `https`-rule hosts work | The public roots are missing: `SSL_CERT_DIR` is set, or a bundle-file variable points at the projection alone |
| Both fail | Wrong file, or an image with no system bundle at the path the wrapper reads |
| Worked, then fails after a resume | The CA rotated and the process still holds its old pool; restart the process |

**A change to the bundle reaches the file, not the process.** atelet resolves
the bundle on `Run` and `Restore` and rewrites the projected file of a running
actor when the bundle changes. A process that has already loaded the file keeps
what it loaded: Go caches its system pool after first use, a JVM loads its
truststore at start, and an actor restored from a snapshot — its template's
golden snapshot included — restores the anchors its process held when the
snapshot was taken. Rotate the CA as described below, which never depends on
an actor reloading its anchors.

**The bundle is not a substitute for authenticating the actor.** It lets the
actor verify the *gateway*. It says nothing to an origin about which actor is
calling; see `cmd/atenet/internal/router/README.md` for that direction.

## Rotate the CA

The gateway signs with the `egress-mitm-ca-pool` Secret in `ate-system`: the
pool (`pool`), from which atecontroller derives the bundle, and the signing
CA's certificate and key (`tls.crt`, `tls.key`), which agentgateway mints
from. Never replace or recreate that Secret to rotate the CA. Deleting it
deletes the bundle, and every actor that starts before it is back fails; and a
replacement reaches actors within seconds but the gateway only after kubelet
refreshes its Secret volume, about a minute later, so every TLS connection in
between fails with `x509: certificate signed by unknown authority`.

Rotate it in three steps instead, from CA `1` to CA `2`:

```bash
# 1. Add the new CA. Its root joins the bundle; the current CA keeps signing.
kubectl ate admin add-ca --secret-namespace ate-system --name egress-mitm-ca-pool --ca-id 2

# 2. Make it sign. The gateway switches once kubelet refreshes its volume.
kubectl ate admin activate-ca --secret-namespace ate-system --name egress-mitm-ca-pool --ca-id 2

# 3. After 20 minutes, retire the old CA. Its root leaves the bundle.
kubectl ate admin retire-ca --secret-namespace ate-system --name egress-mitm-ca-pool --ca-id 1
```

`kubectl ate admin get-ca-pool --secret-namespace ate-system --name
egress-mitm-ca-pool` shows the pool's CAs, which one signs and for how long.

No step waits for actors. `add-ca` has the signing CA certify the new CA's
key as well, and once the new CA signs, the gateway presents that
cross-certificate with every leaf. An actor that trusts only the old root —
one that loaded its anchors before step 1 and never reloads them — verifies
the new leaves through it, and so does an actor restored from a snapshot taken
before the rotation. `activate-ca` refuses a CA that the signing CA did not
certify this way. Steps 1 and 2 can therefore follow each other directly. The
new root and its cross-certificate are dated from the old root's start, so an
actor whose clock lags behind the rotation, such as a micro-VM that has just
resumed from an older snapshot, accepts them whenever it accepts the old root.

Step 3 waits for the gateway, not for actors. Until the gateway has loaded the
new CA, and until every leaf the old CA signed has expired, a leaf that chains
only to the old root may still be served, while actors that start after step 3
no longer trust it. `retire-ca` therefore refuses until the new CA has been
signing for `--min-signing-age`, 20 minutes by default: the Secret volume
refresh, agentgateway's 5-minute leaf cache and sdsmint's 15-minute leaves.

The cross-certificate stays in the chain after step 3, so an actor that still
holds only the old root keeps working until the old root expires or the next
rotation replaces the cross-certificate. An actor process has to load its
anchors anew within one rotation: restart long-lived actors, and let
templates take a new golden snapshot, before the rotation after next.

If the signing CA has already expired, nothing verifies against it and there
is no trust to carry over: `add-ca` adds the new CA without cross-certificate,
`activate-ca` accepts it, and actors recover as they load the new bundle.

## See also

* [API Configuration Guide](api-guide.md) — the full `systemInfo` volume
  reference.
* `demos/egress/README.md` — how tunneled egress and actor-identity
  authentication fit together.
* `cmd/atenet/internal/router/README.md` — the gateway side of the MITM leg.
