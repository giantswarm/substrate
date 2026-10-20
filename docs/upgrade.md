# Rolling upgrade runbook

This runbook upgrades a running Agent Substrate install to a new build
version, one node at a time. No actor loses state. On a cluster with
more than one node, at most one node's worth of capacity is out of
service while the rest of the fleet keeps serving; a single-node cluster
stops fully during step 5. It needs `kubectl`, `kubectl ate`, `go run
./cmd/ate-setup`, `jq`, and `grpcurl`. The numbered steps are the same
on every Kubernetes provider; the provider-specific parts sit in their
own sections, [before](#on-gke) and [after](#after-the-roll-on-gke)
the roll. All of the roll's state lives in cluster objects, so you can
stop at any point and pick up again. [Check your
progress](#check-your-progress) tells you where you left off.

The order is `ate-controller` first, then the dataplane, then the rest
of the control plane. The controller goes first because it manages
the worker pools the roll creates. The dataplane goes before the rest
of the control plane so that, by the time ate-api-server and atenet
change, every atelet and worker already understands requests from
either version of the control plane.

## Check your progress

Come back here after a break, and before you touch anything when
something looks wrong. Three commands say where the roll stands:

```bash
# One DaemonSet: only the old dataplane is installed, and its label
# value is the $OLD_VERSION you are upgrading from. Two: step 3 is done,
# and the other label value is the version you are upgrading to.
kubectl get ds -n ate-system -l app=atelet -L ate.dev/substrate-version

# Nodes per version. All on the old one: the per-node roll has not
# started. A mix: it is under way. All on the new one: it is done.
kubectl get nodes -L ate.dev/substrate-version --no-headers \
  | awk '{print $NF}' | sort | uniq -c

# Worker pools. A clone next to each serving pool: step 4 is done.
kubectl get workerpools -A
```

The commands above do not show whether steps 1, 2 and 6 have run.
Those steps are idempotent, so run them again if you are not sure.

## Before you start

### Confirm before you begin

Every item has to hold. Nothing in the roll stops you if one does not.

- [ ] Every node carries the `ate.dev/substrate-version` label, all
  with the same value. That value is `$OLD_VERSION`.

  ```bash
  kubectl get nodes -L ate.dev/substrate-version
  ```

- [ ] The atelet DaemonSet name carries a version suffix. If it does
  not, the cluster was installed from an older build and needs a fresh
  install instead; its DaemonSet selector cannot be changed in place.

  ```bash
  kubectl get ds -n ate-system -l app=atelet
  ```

- [ ] Every serving pool is pinned to `$OLD_VERSION`. An unpinned pool
  cannot take part in the roll.

  ```bash
  kubectl get workerpools -A \
    -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,PIN:.spec.template.nodeSelector.ate\.dev/substrate-version'
  ```

  Pin a pool whose `PIN` column is `<none>` while no actor is assigned
  to its workers, because the edit re-renders the pool's Deployment
  (see [the warnings](#three-things-that-break-an-upgrade)):

  ```bash
  kubectl -n $NS patch workerpool $OLD_WORKERPOOL --type merge \
    -p "spec: {template: {nodeSelector: {ate.dev/substrate-version: '$OLD_VERSION'}}}"
  ```

- [ ] Every serving pool is healthy: `READY` equals `DESIRED`.

  ```bash
  kubectl get workerpools -A
  ```

- [ ] Nothing drains worker nodes on its own: node auto-upgrade is off
  on every pool that runs workers and none of them is spot or
  preemptible, as the
  [Create Cluster warning](../tools/setup-gcp/README.md#2-create-cluster)
  requires.

- [ ] If the install uses `--cordon-control-plane` with the bundled
  PostgreSQL, at least one node carries
  `ate.dev/workloadType=ate-postgres`. Releases before the dedicated
  postgres pool ran postgres in the shared `ate-control-plane` pool; see
  [Cordoned control plane: postgres pool](#cordoned-control-plane-postgres-pool).
  `ate-setup` refuses to apply postgres without one.

  ```bash
  kubectl get nodes -l ate.dev/workloadType=ate-postgres
  ```

- [ ] The installed ate-api-server serves `DrainWorker`. Draining a
  worker is one `DrainWorker` RPC, and step 5 calls it with
  [grpcurl](https://github.com/fullstorydev/grpcurl). Open access to
  the Control API now and keep the port-forward running; step 5 uses
  the same `/tmp/ate-ca.pem` and `$TOKEN`.

  ```bash
  kubectl -n ate-system port-forward svc/api 8443:443 >/dev/null 2>&1 &

  kubectl get clustertrustbundles -l podcert.ate.dev/canarying=live \
    -o jsonpath='{range .items[?(@.spec.signerName=="servicedns.podcert.ate.dev/identity")]}{.spec.trustBundle}{end}' \
    > /tmp/ate-ca.pem

  TOKEN=$(kubectl -n ate-system create token ate-client \
    --audience=api.ate-system.svc --duration=48h)

  # Must print ateapi.Control.DrainWorker. If it does not, the cluster
  # predates the versioned builds and needs a fresh install.
  grpcurl -cacert /tmp/ate-ca.pem -authority api.ate-system.svc \
    -H "authorization: Bearer ${TOKEN}" 127.0.0.1:8443 list ateapi.Control \
    | grep DrainWorker
  ```

### Names used throughout

| name | what it is | how to get it |
|---|---|---|
| `$OLD_VERSION` | the version label value you are upgrading from | the one atelet DaemonSet's label, see [Check your progress](#check-your-progress) |
| `$NEW_VERSION` | the version label value you are upgrading to | the second DaemonSet's label once step 3 has run |
| `$NS`, `$OLD_WORKERPOOL` | namespace and name of a serving WorkerPool | `kubectl get workerpools -A` |
| `$NEW_WORKERPOOL` | the clone's name | you pick it in step 4, for example `counter-v2` |
| `$NODE` | the node being rolled | picked per iteration in step 5 from `kubectl get nodes` |

A cluster usually serves more than one WorkerPool, and every serving
pool moves in the same upgrade. Step 4 clones each of them, the
per-node roll in step 5 covers all pools on a node together, and the
retire at the end deletes each old pool. Where the runbook says
`$OLD_WORKERPOOL`, read "each serving pool".

### Checkout and environment

Every `go run ./cmd/ate-setup` command below runs from a checkout of
the new release, with the environment and flags the install used:
`PROJECT_ID`, `CLUSTER_NAME` and `CLUSTER_LOCATION` (or `--context`),
and either `KO_DOCKER_REPO` for a build from source or
`--image-repo`/`--image-tag` for prebuilt images. Keep a checkout of
the old release too as rollback runs the same commands from it.

`ate-setup` takes the version from `$VERSION` when it is set, else
from `git describe` on the checkout for a build from source, or from
`--image-tag` (`ATE_IMAGE_TAG`) for prebuilt images. That value must come out different
from `$OLD_VERSION`, otherwise dataplane upgrade rolls the running atelet in place
instead of adding a second DaemonSet. A tagged checkout differs by
construction; if the install pinned `VERSION`, pin a new value now
and keep it for every command of this upgrade.

Install the new `kubectl ate` with `go install ./cmd/kubectl-ate`.

### On GKE

GKE needs `gcloud` and three more names:

| name | what it is | how to get it |
|---|---|---|
| `$CLUSTER`, `$ZONE` | the cluster and its location | `gcloud container clusters list` |
| `$NODEPOOL` | the node pool | `gcloud container node-pools list --cluster $CLUSTER --zone $ZONE` |

Park the autoscaler. During the roll, the old pool's pods that lost
their node sit Pending on purpose: they are the rollback reserve. The
autoscaler reads Pending pods as demand and would add nodes for pods
that must never schedule. Save its config first; you restore it
[after the roll](#after-the-roll-on-gke).

```bash
gcloud container node-pools describe $NODEPOOL --cluster $CLUSTER --zone $ZONE \
  --format='json(autoscaling)'   # save for after the roll
gcloud container clusters update $CLUSTER --zone $ZONE \
  --node-pool $NODEPOOL --no-enable-autoscaling
```

If a GKE maintenance window falls inside the roll, add a maintenance
exclusion for it too: a node GKE recreates mid-roll comes back at the
pool's label, which is still `$OLD_VERSION`.

Check that the node pool itself carries
`ate.dev/substrate-version=$OLD_VERSION`, or nodes GKE creates later
arrive unlabeled and nothing schedules to them. If it is missing, stamp
it now with the old value:

```bash
gcloud container node-pools describe $NODEPOOL --cluster $CLUSTER --zone $ZONE \
  --format='get(config.labels)' # copy the value
gcloud container node-pools update $NODEPOOL --cluster $CLUSTER --zone $ZONE \
  --node-labels=<every existing user label>,ate.dev/substrate-version=$OLD_VERSION
```

Other providers have their own equivalents: stop whatever adds or
recreates worker nodes during the roll, and make sure a node created
later arrives with the old label.

### Cordoned control plane: postgres pool

Skip this section unless the install uses `--cordon-control-plane` with
the bundled PostgreSQL. Under that flag, postgres runs alone in a
one-node pool labeled and tainted
`ate.dev/workloadType=ate-postgres:NoSchedule`, and every other control
plane workload shares the `ate-control-plane` pool. An install from a
release before that split has no postgres pool. The StatefulSet deletes
its running pod before creating the replacement, so without the pool
step 6 would take postgres down with nothing to schedule it on.

Create the pool before step 6, in the zone of the postgres volume. The
volume is a zonal disk, and a postgres pod on a node in another zone
stays Pending with a volume node affinity conflict.

```bash
# The zone the postgres volume lives in.
PG_ZONE=$(kubectl get pv \
  "$(kubectl -n ate-system get pvc data-postgres-0 -o jsonpath='{.spec.volumeName}')" \
  -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[*].matchExpressions[*].values[0]}')
echo "$PG_ZONE"

# A machine type that fits the postgres pod's requests: the size10
# profile asks for 80 CPUs and 140Gi.
gcloud container node-pools create ate-postgres --cluster $CLUSTER --zone $ZONE \
  --node-locations "$PG_ZONE" --num-nodes 1 --machine-type <machine type> \
  --node-labels ate.dev/workloadType=ate-postgres \
  --node-taints ate.dev/workloadType=ate-postgres:NoSchedule
```

On other providers, create the equivalent: one node in the volume's
zone, carrying the label and the taint.

Step 6 then restarts postgres once onto the new node, which is a short
outage for ate-api-server. The other control plane Deployments roll in
steps 2 and 6 as well. Their old pods still carry the required
anti-affinity of the earlier release, so the new pods land on the
`ate-control-plane` pool's spare node first and stay packed there. Once
`kubectl get pods -n ate-system -o wide` shows no pod older than the
roll, spread them again and shrink the pool, which no longer needs one
node per pod:

```bash
kubectl -n ate-system rollout restart deployment \
  ate-api-server ate-controller atenet-router atenet-egress podcertificate-controller
gcloud container clusters resize $CLUSTER --zone $ZONE \
  --node-pool <ate-control-plane pool> --num-nodes <smaller size>
```

## Three things that break an upgrade

Each warning comes back at the step where the mistake becomes possible.

> [!WARNING]
> **Do not delete a node's old worker pods before flipping its
> version label.** Otherwise the old pool reschedules replacements onto
> the same node, and old workers end up next to the new atelet: exactly
> the version skew the roll exists to prevent. (Step 5)

> [!WARNING]
> **Do not edit a serving worker pool, and do not scale it down.** The
> controller would roll the pool's Deployment straight through live
> actors. A deleted worker pod does go through the eviction path:
> `SIGTERM` is forwarded into the actor's containers and the control
> plane keeps accepting a suspend for 30 minutes, so an actor suspended
> inside that window saves its state and stays resumable. Handling
> `SIGTERM` by exiting cleanly is not enough on its own; the suspend has
> to reach the control plane and finish. An actor still awake when the
> window closes moves to `ACTOR_STATE_CRASHED`: `resume` and `suspend`
> are both refused, and everything since its last snapshot is lost. Call
> `RevertActor` (`kubectl ate revert`) to discard the crashed run and
> return the actor to `ACTOR_STATE_SUSPENDED` at its last external snapshot
> so it can be resumed. Scaling a serving pool down removes pods the same
> way, without suspending the actors on them. (Step 4 clones the pool; it
> never edits it.)

> [!WARNING]
> **On GKE, do not touch the node pool's label until every node is
> rolled.** A pool label update applies in place to every node in the
> pool, so the whole fleet flips at once, with no drain and no pacing.
> (After the roll)

## Upgrade

### 1. Apply the new CRDs

From the new release's checkout, apply its CRDs. Nothing running
changes; the new schema is in place for the controller that follows.

```bash
# in a checkout of the new release
kubectl apply -f manifests/ate-install/generated
```

### 2. Upgrade ate-controller

```bash
go run ./cmd/ate-setup deploy ate-controller
```

This rolls `ate-controller` (and re-applies the CRDs, which is a
no-op now).

By convention, changes to how the controller renders worker
pods sit behind WorkerPool fields, so the new controller keeps
rendering the serving pools as they are. A release that breaks that
convention says so in its notes. Expect the pools' Deployments to
roll once here in that case. Every actor is suspended through the
worker eviction path, loses no state, and resumes on demand. If they
roll, wait for `READY` to equal `DESIRED` again on every serving pool
(`kubectl get workerpools -A`) before step 4.

The move of the node state root from `/var/lib/ateom-gvisor` to
`/var/lib/ate` is such a release. A worker that lands on a node whose
atelet still uses the old path reaches it only once step 5 moves that node.
The old directory can be deleted afterwards.

### 3. Prepare the new dataplane

The dataplane roll starts with the new atelet DaemonSet, from the
same checkout:

```bash
go run ./cmd/ate-setup deploy atelet
```

It lands next to the old one with zero pods until step 5 flips a
node.

Then read `$NEW_VERSION` off the cluster:

```bash
kubectl get ds -n ate-system -l app=atelet -L ate.dev/substrate-version
```

Exactly two DaemonSets print. The SUBSTRATE-VERSION that is not `$OLD_VERSION` is
`$NEW_VERSION`, and the new DaemonSet shows 0 `DESIRED` because no node carries
its label yet. `kubectl get nodes -L ate.dev/substrate-version` still
shows every node at `$OLD_VERSION`. If only one DaemonSet prints, the
version did not change (see Checkout and environment) and the command
rolled the running atelet in place.

Last, build and push the new worker images. The refs print together
at the end. The one for your pool's `sandboxClass` goes into the clone
in step 4:

```bash
go run ./cmd/ate-setup publish worker-images
```

If you are using prebuilt images there is nothing to publish. The new worker image
is the release's `ateom-<sandboxClass>` image under the same repo and
tag as the control plane, pinned by digest:

```bash
NEW_IMAGE=$IMAGE_REPO/ateom-gvisor:$IMAGE_TAG@$(crane digest $IMAGE_REPO/ateom-gvisor:$IMAGE_TAG)
```

### 4. Create the new pool

**Repeat this step for every serving pool the checklist listed**.

> [!WARNING]
> Clone the old pool. Do not edit it. An edit rolls its Deployment
> straight through the live actors on it (second warning above).

Copy the old pool under a new name (`$NEW_WORKERPOOL`, for example
the old name plus `$NEW_VERSION`) and change only `workerImage` and
the version pin. The command below does exactly that; everything
else, including the `metadata.labels` the scheduler matches actors
by, carries over as is.

```bash
NEW_IMAGE=<the ateom ref from step 3, for this pool's sandboxClass>

kubectl -n $NS get workerpool $OLD_WORKERPOOL -o json \
  | jq --arg name "$NEW_WORKERPOOL" --arg image "$NEW_IMAGE" --arg version "$NEW_VERSION" '
      {apiVersion, kind,
       metadata: {name: $name, namespace: .metadata.namespace,
                  labels: .metadata.labels},
       spec: (.spec + {workerImage: $image})}
      | .spec.template.nodeSelector["ate.dev/substrate-version"] = $version
    ' \
  | kubectl apply -f -
```

Do not shrink `spec.template.resources.limits` while cloning. A
worker only accepts an actor whose limits fit under them.

Verify:

```bash
# $NEW_VERSION, then an image ref that contains @sha256:
kubectl -n $NS get workerpool $NEW_WORKERPOOL \
  -o jsonpath='{.spec.template.nodeSelector.ate\.dev/substrate-version}{"\n"}{.spec.workerImage}{"\n"}'

# One Deployment per pool; new-pool pods all Pending (no node carries
# the new label yet).
kubectl -n $NS get deploy -l ate.dev/worker-pool
kubectl -n $NS get pods -l ate.dev/worker-pool=$NEW_WORKERPOOL
```

While both pools serve, placement between them is random, and that
is fine: a snapshot written on either version restores on either
version. The roll converges because step 5 takes old workers out of
service node by node, not because the scheduler prefers the new pool.

One exception: the release that starts honoring an image's `USER` and
`WORKDIR`. `runsc restore` checks both against the snapshot for every
container, and the pause image sets `USER 65535`, so no gVisor snapshot
crosses that upgrade in either direction. Every gVisor actor loses its
state: delete them before the roll, and recreate every gVisor template
after it so each takes a new golden snapshot. Micro-VM snapshots are
not checked. They resume as they were, and the new identity applies
from the next cold boot; files an earlier run wrote as root into a
durable dir stay root-owned, readable but not writable by a process
that now runs as another user.

### 5. Roll each node

Repeat for every node, one at a time. On a single-node cluster this
step is a full stop: every actor is suspended at once.

**a. Drain the node's workers.** Bound actors keep running. Draining
only stops new placements.

```bash
# A worker's name is its pod's UID; that is what DrainWorker takes.
for w in $(kubectl ate get workers -o json \
             | jq -r --arg node "$NODE" '.workers[] | select(.nodeName == $node) | .metadata.name'); do
  grpcurl -cacert /tmp/ate-ca.pem -authority api.ate-system.svc \
    -H "authorization: Bearer ${TOKEN}" \
    -d "{\"worker\": {\"name\": \"${w}\"}}" \
    127.0.0.1:8443 ateapi.Control/DrainWorker
done
```

**b. See what is still on the node.** Two lists: the node's workers
with the actor each one hosts, and any paused actor whose local
snapshot lives on this node. A paused actor sits on no worker, so it
shows up only in the second list.

```bash
kubectl ate get workers -o json | jq -r --arg node "$NODE" '
  ["WORKER", "POD", "ASSIGNED ACTOR"],
  (.workers[] | select(.nodeName == $node)
   | [.metadata.name, .workerPod,
      (.status.assignment.actor | if . then .atespace + "/" + .name else "<none>" end)])
  | @tsv' | column -t -s $'\t'

kubectl ate get actors -A -o json | jq -r --arg node "$NODE" '
  ["PAUSED_ACTOR", "STATE"],
  (.actors[]
   | select(.status.assignedNode == $node and (.status.workerAssignment == null))
   | [.metadata.atespace + "/" + .metadata.name, .status.state])
  | @tsv' | column -t -s $'\t'
```

**c. Suspend them at your own pace.** Every actor in either list has to be suspended
before the node moves (`kubectl ate suspend actor <name> -a
<atespace>`). Suspend releases the worker and uploads the durable
snapshot, so the actor resumes on demand onto any free matching worker
afterwards. Repeat step b until the `ASSIGNED ACTOR` column reads
`<none>` throughout and the paused list is empty. Then rerun the drain
in step a once more right before flipping.

> [!WARNING]
> Do not run e before d. Deleting the old-pool pods while the node
> still carries `$OLD_VERSION` lets the old pool put replacements right
> back on this node, next to the new atelet (first warning above).

**d. Flip the label.** The old atelet pod leaves on its own and the
new one starts.

```bash
kubectl label node $NODE ate.dev/substrate-version=$NEW_VERSION --overwrite
```

Wait for the new `atelet` pod on the node to be Ready before going on. Usually that is seconds; the new `atelet` pod stays Pending only while the old one is still exiting, since it holds the node's host ports until then.

```bash
# The pod is named after atelet-<new suffix>.
kubectl get pods -n ate-system -l app=atelet --field-selector spec.nodeName=$NODE
```

**e. Delete the node's old-pool worker pods.** Step c emptied them,
but they are still Ready and hold capacity the new pool needs on this
node. Their Deployment cannot reschedule them here anymore. Repeat
per pool if the node hosts several.

```bash
kubectl -n $NS delete pod -l ate.dev/worker-pool=$OLD_WORKERPOOL \
  --field-selector spec.nodeName=$NODE
```

**f. Confirm the node has moved.** Two checks:

```bash
# New-pool workers came up here.
kubectl -n $NS get pods -l ate.dev/worker-pool=$NEW_WORKERPOOL --field-selector spec.nodeName=$NODE

# No old-pool pod is left here.
kubectl -n $NS get pods -l ate.dev/worker-pool=$OLD_WORKERPOOL --field-selector spec.nodeName=$NODE
```

New-pool pods Pending on other nodes are expected until those nodes
move.

**g. Take the next node.** Start again at step a once `kubectl ate get
workers` shows at least one FREE worker for the suspended actors to
land on.

You are done when `kubectl get nodes -L ate.dev/substrate-version`
shows every node at `$NEW_VERSION` (a node that joined mid-roll still
carries `$OLD_VERSION`; apply step 5 to roll it) and every assigned
actor in `kubectl ate get actors -A` sits on a new-pool pod.

### 6. Upgrade the rest of the control plane

Every actor is now on the new dataplane, running or suspended, and
ate-controller moved in step 2. From the same checkout, move
`ate-api-server` first, then everything else:

```bash
go run ./cmd/ate-setup deploy apiserver
go run ./cmd/ate-setup deploy ate-system --credential-provider='<as installed>'
```

`deploy ate-system` requires the credential provider selection; pass the one
the install was made with, or carry it in `ATE_CREDENTIAL_PROVIDER`. An install
made with `--experimental-egress-credential-injection` and the bundled provider
is `{"name":"k8s.io"}`; one without injection is `{"enabled":false}`. The
selection replaces `--credential-provider-name` and
`--credential-provider-address`: a provider you deploy yourself is
`{"name":"<provider>","address":"<host>:<port>"}`, with the name bare
(`vault.example.com`, not `ate-secret://vault.example.com`).

The second command rolls atenet and converges the rest of the
install, postgres included: on a cordoned install it moves postgres to
its own pool (see [Cordoned control plane: postgres
pool](#cordoned-control-plane-postgres-pool)); it re-resolves and re-applies everything, so it could take a
while. The checklist's port-forward dies when the API server rolls.
Restart it and mint a fresh token if you still need to drain.

**NOTE**: Until
this step is done the old ate-api-server is still serving, so do not
start using API fields new in this release before upgrade finishes.

An install from a release before golden tags recorded each template's
golden snapshot as the golden actor's own external snapshot and kept
that actor suspended on it; since golden tags the same status field
names the golden tag, so such a status reads as a tag without a name.
Once the new ate-api-server serves, its ActorTemplate reconciler
migrates every such template on its first resync
(`--template-resync-interval`, 20 s by default): the golden actor's
snapshot is copied into the template's golden tag and the actor is
released, as a new golden is recorded; a golden whose actor is gone
fails the template as `GoldenSnapshotLost` with what is missing:
recreate the template. Until a template is migrated, a create of an
actor from the template answers `Unavailable`; retry it.

### The 1.7 to 1.8 step

1.8.0 changed the snapshot layout: one rootfs-upper tar per container
and one tar per durable volume, where 1.7 wrote every durable volume
into a single `durable-dir.tar`. **Step from 1.7 to 1.8.1 or later,
never to 1.8.0**: 1.8.0 refuses to restore a 1.7 durable snapshot
([#265](https://github.com/giantswarm/substrate/pull/265)), so every
actor suspended on 1.7 crashes on its first resume there. From 1.8.1
the step migrates automatically, with no snapshot reset and no manual
step; conversations and durable volumes survive it:

- Each actor's durable snapshot in the 1.7 layout is converted to the
  per-volume layout on its first resume, and its next checkpoint writes
  the new layout ([#277](https://github.com/giantswarm/substrate/pull/277)).
  A snapshot that does not convert is refused with `FailedPrecondition`
  and the cause instead of restoring empty volumes.
- Once the new ate-api-server serves, its ActorTemplate reconciler takes
  every golden written in an older layout again on the new workers, in
  the current layout, and deletes the old one
  ([#282](https://github.com/giantswarm/substrate/pull/282)). Until a
  template's new golden is complete, a create of an actor from the
  template answers `Unavailable`; retry it.

The [snapshot reset runbook](snapshot-reset.md) is not part of this
step: it stays the remedy for a corrupted snapshot store only.

### Patch steps within 1.8

Every released 1.8 patch has an entry here, newest first. A step from one
1.8 patch to the next follows the numbered steps above with no extra
action. None of the entries below changes the Postgres schema or needs a
[snapshot reset](snapshot-reset.md), and none loses a conversation: an
actor's snapshots are read as before, and a stored row an older patch
wrote in another shape is rewritten when it is read, with no step to run.
A new 1.8 patch adds its entry here.

#### 1.8.4 to 1.8.5

The step rolls by the range of changes `v1.8.4..v1.8.5`.

- **Persisted format:** unchanged. No Postgres schema, CRD or chart value
  differs between the two tags. The atelet RPC gains one field,
  `ExternalVolumeSource.read_only` (field 5), which ate-api sets and atelet
  reads; neither side stores it.
- **Migration, automatic:** none to run.
- **Resume of an actor the trust-bundle guard crashed, automatic:** an
  actor whose template row 1.8.3 had rewritten without its trust-bundle
  names was recorded `CRASHED` on a resume with `no trust bundle names
  given`, and stayed so after 1.8.4 read the template with its name again
  ([#332](https://github.com/giantswarm/substrate/issues/332),
  [#335](https://github.com/giantswarm/substrate/pull/335)). Such a crash
  is now outlived: when the template, as the store reads it, names a bundle
  in every trust-bundle projection, the actor is returned to its last
  snapshot (`SUSPENDED`, or `PAUSED` while it holds its pause snapshot)
  with its worker freed, and the next resume places it on a worker. The
  `ActorTransitionReconciler` recovers one no request resumes, on the first
  resync. The actor never ran since that snapshot, so the return loses
  nothing. An actor whose template still names no bundle stays `CRASHED`
  and is not retried in a loop.
- **Read-only volume mounts:** a read-only mount of an existing volume asks
  the CSI driver for the access mode of the volume's own PersistentVolume,
  as the kubelet does, and publishes it read-only, where 1.8.4 asked for
  `MULTI_NODE_READER_ONLY` even on a `ReadWriteMany` volume. Drivers that
  accept only the PersistentVolume's mode (the AWS EFS driver) no longer
  fail `NodePublishVolume` with `invalid access mode`. A `ReadOnlyMany`
  volume still mounts as before.
- **Reset:** none. **Conversations lost:** none.

#### 1.8.3 to 1.8.4

- **Persisted format:** unchanged. No proto field, no CRD and no chart
  value differs between the two tags.
- **Migration, automatic:** the ActorTemplate row migration of 1.8.3
  (`migrateLegacyActorTemplate` in `cmd/ateapi/internal/store/atepg`)
  covers one more case. A trust-bundle projection that carries neither
  `names` nor the legacy single `name` is a row that 1.8.3's first status
  write had already rewritten without its names: it now reads as the one
  bundle `egress-mitm.ate.dev`, the only name a release that stored the
  legacy field accepted. A projection that carries names is left alone.
  The rewritten template is persisted by the template's next update.
  Before this, every boot from such a template failed in atelet with `no
  trust bundle names given`.
- **Golden snapshots retaken, automatic:** the ActorTemplate reconciler
  clears a golden failure that 1.8.3 or earlier recorded as terminal
  (`GoldenActorUnexpectedState`, or a `GoldenActorCrashed` for a missing
  trust-bundle name when every projection now names a bundle) and takes
  the golden again; the golden recorded beside the failure stays the
  template's golden until the new one is complete. A golden actor found
  `DELETING`, `PAUSING`, `PAUSED` or `REVERTING` before its snapshot (a
  `DELETING` one is a delete that a rollback of ate-api cut) is released
  and the take retried instead of failing the template. A create from the template answers
  `Unavailable` until its golden is complete; retry it.
- **Interrupted suspends and pauses, automatic:** a suspend or pause that
  a rolling worker pool interrupts while it checkpoints the running
  workload now answers `Unavailable`; the actor is returned to its last
  external snapshot, `SUSPENDED`, with its worker freed and the
  half-written snapshot discarded, and the caller's retry resumes it. The
  `ActorTransitionReconciler` does the same for an actor an earlier call
  left `CRASHED` this way, so an actor 1.8.3 left crashed is recovered on
  the first resync. Only a crash that holds an external snapshot is
  recovered; the workload went with its worker, so the return loses
  nothing the crash had not already lost. An actor in any other state is
  left alone.
- **Reset:** none. **Conversations lost:** none.

#### 1.8.2 to 1.8.3

- **Persisted format:** the `WorkerPool` CRD gains two optional status
  fields, `workerImage` and `conditions` (`WorkerImageOverridden`), and an
  `Image` printer column; the chart's role grants ate-controller `create`
  and `patch` on `events.k8s.io` events. Apply the new CRDs in step 1 as
  usual. The Postgres schema and the protos are unchanged.
- **Migration, automatic:** an ActorTemplate row written before the
  trust-bundle projection took repeated `names` held its one bundle in the
  old `name` field, which the current descriptor drops, so the projection
  read as naming no bundle. The store now rewrites such a row when it
  reads it: the old `name` becomes the one entry of `names`; the rewrite
  is persisted by the template's next update. atelet refuses a boot from a
  projection that names no bundle with `no trust bundle names given`,
  where it used to index the empty list.
- **Resume retries:** a resume that a worker drain cuts answers
  `Unavailable` naming the crash, also while the actor's recovery is
  unfinished; the next attempt or the `ActorTransitionReconciler` returns
  the actor to the snapshot it was resuming, and the caller's retry
  resumes it.
- **Chart:** the atelet DaemonSet takes `atelet.updateStrategy`, default
  `RollingUpdate` with `maxUnavailable: 25%`; `maxSurge` must stay 0 and
  the chart refuses any other value.
- **Reset:** none. **Conversations lost:** none.

#### 1.8.1 to 1.8.2

- **Persisted format:** unchanged. No proto, CRD, chart or Postgres schema
  change.
- **Migration, automatic:** none to run. New behaviour on the first
  resync: an actor that a resume crashed for a cause a rolling worker pool
  produces is returned to the snapshot it was resuming (`PAUSED` while it
  holds its pause snapshot, else `SUSPENDED`) by the
  `ActorTransitionReconciler`, and a resume or golden take such a pool
  interrupts is retried with backoff instead of failing the actor or the
  template. A golden failure recorded as a transient crash is cleared and
  the golden taken again.
- **Reset:** none. **Conversations lost:** none.

#### 1.8.0 to 1.8.1

The step is described in [the 1.7 to 1.8 step](#the-17-to-18-step): 1.8.1
is the first 1.8 release a 1.7 install can step to, and never to 1.8.0.
Between the two tags the only persisted-format change is the additive
`snapshot_layout` field on `GoldenSnapshotStatus`, which records the
layout a golden tag was written in; a golden recorded without it is taken
again in the current layout, as that section says. No reset and no lost
conversation.

### The 1.8 to 1.9 step

1.9.0 moves the line onto upstream v0.5.0-alpha2. Step to it from 1.8.4
with the numbered steps above; the step migrates on its own, with no
snapshot reset and no manual step, and conversations and durable volumes
survive it.

- **Persisted format:** the Postgres schema is unchanged (no file in
  `migrations/` or `migrations_fork/` differs). In the protos,
  `LocalSnapshot.content_scope` moves from field 3 to field 2, the field
  1.8 used for `node_vms_with_local_snapshots`; `LocalSnapshot.node_name`
  (5) and `ActorStatus.assigned_node` (10) are new;
  `SnapshotConfig.on_pause` is removed
  ([agent-substrate/substrate#2309](https://github.com/agent-substrate/substrate/pull/2309)),
  so a pause captures the `on_commit` scope; Pause, Suspend and Resume take
  an optional `FencingToken` (field 10001) and ate-api records the newest
  one in `ActorStatus.fencing_token` (10001). The `SandboxConfig` CRD
  requires `pauseImage` for `gvisor` only and refuses it for every other
  sandbox class; the `WorkerPool` CRD gains an optional
  `serviceAccountName` in its pod template. Apply the new CRDs in step 1 as
  usual.
- **Migration, automatic:** an Actor row a 1.8 release wrote is rewritten
  when it is read (`migrateLegacyLocalSnapshot` and
  `migrateLegacyAssignedNode` in `cmd/ateapi/internal/store/atepg`): a
  local snapshot takes its node from the 1.8 node list into `node_name`
  and its content scope from the 1.8 field 3, and `assigned_node` takes
  the worker assignment's node, else the local snapshot's. The rewrite is
  persisted by the actor's next update. A paused or suspended 1.8 actor
  resumes on its node with its snapshot's scope as written.
- **Fencing:** a request without a `FencingToken` is admitted as before,
  so a client that sends none behaves as on 1.8.
- **Removed `on_pause`, accepted for a transition:** ate-api accepts
  `SnapshotConfig.on_pause` (field 1) in a request, ignores its value and
  returns the template with it as it was written, so a client still built
  against the 1.8 protos (kagent 1.10.x) keeps creating and editing
  templates and reads back what it wrote. It logs `Request carries a
  removed field; ignoring it` once per client, naming the field, the
  method and the principal. The transition ends once no client sends it.
  Every other unknown field is still refused.
- **Before step 1:** a `SandboxConfig` of a sandbox class other than
  `gvisor` that sets `pauseImage` is refused by the new CRD on its next
  create or update; drop the field from such objects. The chart renders
  only the `gvisor` one, which keeps it.
- **Chart:** every component now has default requests and limits; the
  memory limits are atelet, ate-api-server, atenet-router's agentgateway
  and rustfs 1Gi, ate-controller 512Mi, the credential provider and the
  rustfs bucket-init job 256Mi. An installation that sets its own
  `resources` keeps them. The controller Role is created in the release
  namespace, and rustfs runs under a ServiceAccount of its own.
- **Reset:** none. **Conversations lost:** none.

### Patch steps within 1.9

Every released 1.9 patch has an entry here, newest first, in the shape of
the 1.8 entries. A step from 1.8.4 straight to a 1.9 patch is the 1.8 to
1.9 step above plus the entries up to that patch.

#### 1.9.0 to 1.9.1

- **Persisted format:** unchanged. No proto field, no CRD and no Postgres
  schema differs between the two tags.
- **Chart:** the default `images.agentgateway`, the dataplane of
  atenet-router and the egress gateway, moves from the agentgateway line's
  2.3.0 to 2.3.5; both roll on the upgrade. An installation that sets its
  own `images.agentgateway` keeps it.
- **Object storage:** a process runs at most two large snapshot transfers
  at once (an upload of more than one part, a ranged download); the others
  wait for a slot. Objects of one part never wait. Suspends that a node's
  actors start together no longer buffer one image each, which on 1.9.0
  could outgrow atelet's 1Gi limit and restart it.
- **Metrics:** the router records `ate.router.outcome=denied` on
  `atenet.router.route.duration` for a 403 and a `PermissionDenied`, which
  1.9.0 counted as `resume_error`. An alert on `resume_error` no longer
  fires on policy refusals.
- **Migration:** none to run.
- **Reset:** none. **Conversations lost:** none.

### The 1.9 to 1.10 step

1.10.0 stays on upstream v0.5.0-alpha2 and adds the credential path
prefix to the egress contract. Step to it from 1.9.1 with the numbered
steps above; the step needs no migration, no snapshot reset and no
manual step, and conversations and durable volumes survive it. A step
from 1.8.5 straight to 1.10.0 is the 1.8 to 1.9 step, the 1.9.0 to 1.9.1
entry and this one in one roll: the 1.8 rows are migrated on read as
that step describes, nothing else runs.

- **Persisted format:** the Postgres schema is unchanged (no file in
  `migrations/` or `migrations_fork/` differs from 1.9.1), and so are the
  CRDs and the snapshot layout. In the protos, `CredentialHeader` gains
  the optional `path_prefix` (field 5): a stored egress policy written
  before the step has no prefix, reads back as written and applies
  host-wide, as before.
- **Egress contract:** `GetEgressPolicyContract` answers
  `v0.4.0-alpha1+path-prefix`. A `kubectl-ate` refuses to write an egress
  policy to an ate-api of another contract, so after the step write
  policies with a 1.10 client. Every entry without a `path_prefix` behaves
  as on 1.9; the egress gateway (the agentgateway line's 2.3.5, the 1.9.1
  default) applies a prefixed entry to the requests under its prefix only.
- **Removed `on_pause`:** still accepted for a transition, as in the 1.8
  to 1.9 step.
- **Lost workers:** a running actor whose worker pod goes away, or whose
  ateom restarts, with no operation in flight, returns to its own durable
  snapshot and resumes elsewhere on its next resume, where 1.9 recorded it
  `CRASHED`; an actor that never suspended is still recorded `CRASHED`,
  since its only snapshot is the template's golden. A resume whose worker
  starts draining or goes away while the restore runs is retried on
  another worker instead of answering `RUNNING` there. A rolling worker
  pool during the step therefore costs a turn in flight at most, never the
  conversation up to the last checkpoint.
- **Goldens:** taken again only when the snapshot layout changes, which
  this step does not: the goldens 1.9 recorded are kept.
- **Chart:** unchanged from 1.9.1, `images.agentgateway` included.
- **Proven:** a step from 1.8.5 to 1.10.0-rc.5 while every worker pod of
  the pool was deleted every 8 s for a minute: a suspended agent with a
  three-turn conversation and a file on its durable volume resumed during
  that drain (the resume retried past the leaving workers and answered
  `READY`), answered from its first turn and read the file written on
  1.8.5; its template's golden stayed in layout 2 and a new conversation
  started from it.
- **Reset:** none. **Conversations lost:** none.

### Patch steps within 1.10

Each patch step below needs no migration, no snapshot reset and no
manual step; no conversation and no durable volume is lost. A step from
1.8.5 or 1.9.1 straight to a 1.10 patch is the 1.9 to 1.10 step and the
patch entries in one roll.

#### 1.10.0 to 1.10.1

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs between the two tags.
- **Chart:** unchanged.
- **Assignment:** when workers exist and none carries the labels of an
  actor's worker selectors, the resume answers `ResourceExhausted` with
  `no worker matches the selector "<selector>" (none of the <n> worker(s)
  carries its labels)` instead of `no worker has room for the actor`.
  Only the message changes: the status, the router's parking and the
  scheduler metric's outcome stay those of a full pool.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.10.1 to 1.10.2

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs between the two tags.
- **Chart:** unchanged.
- **Suspend during a worker's termination:** a suspend whose checkpoint
  completed while the worker's pod went away commits the snapshot and
  answers with the `SUSPENDED` actor. Before, when the worker's record
  was deleted between the suspend's read of the worker and its release,
  the suspend answered `NotFound` ("Actor … not found") for an actor that
  exists. The same holds for the release in a crash, a delete and a
  revert.
- **Pause during a worker's termination:** a pause whose worker's record
  was deleted under its release answers with the actor the worker's
  delete crashed, instead of `NotFound`. Before, it answered that the
  actor does not exist.
- **Missing records on Pause, Resume, Suspend and Revert:** `NotFound`
  ("Actor … not found") answers only an actor that does not exist. Any
  other record the operation read that is gone (its worker, a worker's
  claim) answers `Aborted` with `a record the operation read is gone for
  Actor …, please retry`.
- **Crash record of a suspended actor:** a suspend whose checkpoint
  completed after the actor crashed commits `SUSPENDED` with no crash
  record. Before, the actor kept the crash it survived in
  `status.crash`. An actor stored with such a record reads without it
  from the new ate-api-server on; its next update persists that.
- **Crash record of a paused actor:** a pause whose checkpoint completed
  after the actor crashed, with the local checkpoint left in place,
  commits `PAUSED` with that snapshot and writes no crash record. Before,
  the commit kept the crash in `status.crash`, which the read migration
  above hid. A crash that dropped the local checkpoint still stands.
- **gVisor boot whose sandbox did not take the actor's network:** ateom
  fails the boot right after the pause container starts or is restored,
  with `the sandbox did not take the actor's network: eth0 in its
  namespace still carries 169.254.17.2`, instead of the wakeup probe
  being refused until its deadline. The cause is transient: a golden
  take is retried instead of failing the template with
  `GoldenActorCrashed`, and a resume that crashed for it returns the
  actor to its snapshot. A missed wakeup probe also states whether the
  sandbox holds the actor's network. A template that failed before the
  roll keeps its recorded failure: an older ateom's message names no
  network cause.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.10 to 1.11 step

1.11.0 stays on upstream v0.5.0-alpha2 and injects actor JWTs on egress.
Step to it from 1.10.2 with the numbered steps above; the step needs no
migration, no snapshot reset and no manual step, and conversations and
durable volumes survive it. A step from 1.10.0 or 1.10.1 is the 1.10
patch entries and this one in one roll.

- **Persisted format:** unchanged. No CRD, no Postgres schema and no
  snapshot layout differs from 1.10.2. In the protos,
  `MintActorJWTRequest.audience` is named `audiences`, the same field
  number; a stored egress policy reads back as written.
- **Egress contract:** unchanged, `v0.4.0-alpha1+path-prefix`.
- **Actor JWT injection:** an egress rule whose `replace_headers` entry
  has an `actor_jwt` source is now served on both dataplanes. For a
  request that carries the entry's header, the gateway writes the entry's
  prefix plus a JWT minted through `Control.MintActorJWT` for the actor,
  with the entry's audiences and lifetime (subject
  `actor/<atespace>/<actor>`). Before, the Envoy gateway denied such a
  request with `501` and the agentgateway dataplane with `403 invalid
  credential URI: `. A failed mint denies the request: `403` when the
  actor is gone, `503` when ate-api is unavailable or the mint timed out,
  `500` otherwise. No credential provider is needed.
- **Chart:** `images.agentgateway` is the agentgateway line's
  `2.4.0` (2.3.6 + the actor JWT injection).
- **Busy worker marking:** ate-controller can mark the worker pods that
  host actors (`--busy-worker-annotations`, chart
  `ateController.busyWorker.annotations`, off by default): while a Worker
  reports an allocated actor its pod carries the given annotations, for
  Karpenter `karpenter.sh/do-not-disrupt: "true"`, and the label
  `ate.dev/busy=true`; both go once it hosts none. ate-controller's
  ClusterRole gains `patch` on pods. Turned on, a node roll (this one
  included) waits for a busy worker's actors, or for the NodePool's
  `terminationGracePeriod`, before Karpenter drains its node.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### Patch steps within 1.11

Each patch step below needs no migration, no snapshot reset and no
manual step; no conversation and no durable volume is lost. A step from
1.10 straight to a 1.11 patch is the 1.10 to 1.11 step and the patch
entries in one roll.

#### 1.11.0 to 1.11.1

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs between the two tags.
- **Chart:** unchanged.
- **Terminate after a failed restore:** atelet records an actor's
  sandbox (`sandbox-assets.json`) before ateom restores it, as it does
  before a run, and a terminate of an actor without that record skips
  ateom and goes on with the node cleanup. Before, a restore that failed
  in ateom (its sandbox died during `runsc restore`) left no record, and
  every terminate of the actor failed with `failed to read sandbox
  record`: a revert stayed in `REVERTING` and a delete answered
  `FailedPrecondition` (`could not be torn down`). Actors stuck that way
  on 1.11.0 revert or delete once their node's atelet runs 1.11.1.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.11.1 to 1.11.2

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs between the two
  tags. Two RPCs are added: `AteomSupport.ReportActorExit` (ateom to
  atelet) and `WorkerService.ReportActorExit` (atelet to
  ate-api-server).
- **Chart:** unchanged.
- **Sandbox exit of a running actor:** ateom watches the sandbox of every
  actor it hosts and reports one that exits while no lifecycle operation
  stopped it; ate-api-server crashes the actor with `sandbox exited while
  the actor was running: sandbox process <pid> exited`, and its client's
  revert and resume take it from there. Before, such an actor stayed
  `RUNNING` with nothing behind it, and every turn sent to it waited out
  the client's deadline.
- **During the roll:** a report that reaches an atelet or ate-api-server
  still on 1.11.1 is answered `Unimplemented` and logged by ateom; the
  actor stays as it was, as on 1.11.1. Reports land once the worker, its
  node's atelet and ate-api-server all run 1.11.2.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.11.2 to 1.11.3

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs between the two
  tags.
- **Chart:** unchanged.
- **A request to a crashed actor:** ate-api-server's refusal to resume an
  actor in `CRASHED` (`FailedPrecondition`, its message unchanged) carries
  an AIP-193 `ErrorInfo` in the `substrate.dev` domain, reason
  `ACTOR_CRASHED`, metadata `resumable: "false"`. atenet-router's
  agentgateway and its Go resumer answer such a refusal at once instead of
  retrying it for the whole park budget (30 s, 36 calls in the lab), so
  the client's revert of the actor starts right away. A gRPC caller gets
  the refusal's code and details; an HTTP caller `400` from agentgateway.
- **During the roll:** an ate-api-server still on 1.11.2 sends the refusal
  without the directive, and the router keeps the old wait for it.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.11.3 to 1.11.4

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs between the two
  tags.
- **Chart:** unchanged.
- **A CONNECT tunnel to an actor:** atunnel ends the tunnel as soon as the
  actor closes its connection, instead of once the router also stops
  sending. A call the actor answers and closes, such as a gRPC call to an
  HTTP/1.1-only actor, now fails at once instead of waiting for its
  client's timeout.
- **During the roll:** the relay runs in the worker's ateom, so an actor
  on a worker still on 1.11.3 keeps the old relay until its pool rolls.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.11 to 1.12 step

1.12.0 stays on upstream v0.5.0-alpha2. Step to it from the latest 1.11
patch with the numbered steps above; the step needs no migration, no
snapshot reset and no manual step, and conversations and durable volumes
survive it.

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs from 1.11.
- **Credential provider namespace selector:** a namespace policy of the
  Kubernetes credential provider can carry an `ownNamespaceSelector` (a
  label selector) in place of `atespace` and `allowedNamespaces`: each
  namespace it matches may be read by the atespace of the same name and
  by no other, matched by the namespace's labels on each request, so a
  namespace created, labeled or unlabeled later needs no chart change and
  no restart. An `allowedNamespaceSelector` beside `atespace` grants that
  atespace every namespace it matches, the explicit opt-in by which
  `ate-golden` reads tenant namespaces. A policy file without either field
  reads and grants as before.
- **Chart:** with an `ownNamespaceSelector` or an `allowedNamespaceSelector`
  in `credentialProvider.namespacePolicies`, the chart adds the ClusterRole
  and ClusterRoleBinding `<release>-k8s-credential-provider-namespace-reader`
  (list and watch on namespaces); without one it renders as before.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### Patch steps within 1.12

Each patch step below needs no migration, no snapshot reset and no
manual step; no conversation and no durable volume is lost. A step from
1.11 straight to a 1.12 patch is the 1.11 to 1.12 step and the patch
entries in one roll.

#### 1.12.0 to 1.12.1

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs between the two
  tags.
- **Chart:** unchanged.
- **Application container exit of a running actor:** ateom waits on every
  application container of an actor it hosts and reports one that exits
  while its sandbox keeps running, as it reports a sandbox exit;
  ate-api-server crashes the actor with `sandbox exited while the actor
  was running: container "<name>" exited with status <n> while the
  sandbox kept running` (or `was killed by signal <n> (<SIG>)`), and its
  client's revert and resume take it from there. Before, the pause
  container kept such a sandbox running, the actor stayed `RUNNING` with
  no process left to answer, and every turn sent to it waited out the
  client's deadline. Each hosted application container holds one idle
  `runsc wait` process in the worker while it runs.
- **During the roll:** the watch runs in the worker's ateom, so an actor
  on a worker still on 1.12.0 is not watched until its pool rolls.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.12 to 1.13 step

1.13.0 stays on upstream v0.5.0-alpha2. Step to it from the latest 1.12
patch with the numbered steps above; the step needs no migration, no
snapshot reset and no manual step, and conversations and durable volumes
survive it.

- **Persisted format:** unchanged. No proto field of a stored record, no
  CRD, no Postgres schema and no snapshot layout differs from 1.12.
- **Actor-JWT issuer served:** the new `ate-idp-server` serves the
  discovery document (`/.well-known/openid-configuration`) and the key
  set (`/openid/v1/jwks`) of the issuer ate-api-server places in every
  actor JWT, `https://idp.<namespace>.svc` by default, so a relying party
  can verify an actor JWT. It reads the `actor-id-jwt-pool` Secret
  ate-api-server signs with and rereads it at most once a minute, so a
  key rotation in the Secret reaches the key set without a restart.
- **Chart:** renders the Deployment, ServiceAccount and
  PodDisruptionBudget `ate-idp-server` and the Service `idp` (port 443),
  whose name its servicedns certificate carries; `idpServer.enabled:
  false` leaves them out. ate-api-server is given the issuer explicitly,
  `--actor-jwt-issuer` from `ateApi.actorJWTIssuer` or the `idp`
  Service's URL: for a release named `substrate` that is the issuer it
  used before; another release name prefixes the Service and the issuer
  with it. An issuer set through `ateApi.extraArgs` moves to
  `ateApi.actorJWTIssuer`, so both servers name the same one.
- **During the roll:** the issuer's key set answers once an
  `ate-idp-server` pod is ready; actor JWTs minted before keep verifying,
  since the pool is the one they were signed with.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.13 to 1.14 step

1.14.0 stays on upstream v0.5.0-alpha2 and serves `k8s.io` alone from the
bundled Kubernetes credential provider. Step to it from the latest 1.13
patch with the numbered steps above; the step needs no migration, no
snapshot reset and no manual step, and conversations and durable volumes
survive it. One thing has to be in place before the roll.

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs from 1.13.
- **Egress contract:** unchanged, `v0.4.0-alpha1+path-prefix`.
- **Google access tokens:** the bundled provider no longer serves
  `google-access-token.k8s.io` (the minting carried since 1.8.0). The
  class is a credential provider plugin of its own,
  giantswarm/substrate-credential-providers (`google-access-token`),
  deployed beside Substrate and named in
  `credentialProvider.additionalProviders`
  (`uriAuthority: google-access-token.k8s.io`, `host:
  google-access-token-credential-provider.ate-system.svc:50051`), an
  entry 1.13 and before refuse while the name is bundled. **Before the roll:** the
  plugin is deployed and Ready; the `additionalProviders` entry goes into
  the same chart upgrade as the version step. While the roll is under
  way, a Google access-token fetch the egress gateway still sends to the
  bundled provider is refused: the gateway routes the class to the plugin
  only once it has loaded the new configuration.
- **Chart:** `substrate.egressCredentialProviders` bundles one name;
  `images.agentgateway` unchanged.
- **ate-setup:** renders one provider name in the egress policies.

### The 1.14 to 1.15 step

1.15.0 stays on upstream v0.5.0-alpha2 and counts each actor's DNS
queries. Step to it from the latest 1.14 patch with the numbered steps
above; the step needs no migration, no snapshot reset and no manual
step, and conversations and durable volumes survive it.

- **Persisted format:** unchanged. `WorkloadStatsSample` gains
  `dns_queries` (field 14) on the stats RPC between atelet and the
  worker, which nothing stores; no CRD, no Postgres schema and no
  snapshot layout differs from 1.14.
- **New metric:** atelet exports `ate_actor_stats_dns_queries_total`
  per template and worker pool; the per-actor usage record carries
  `dns_queries`.
- **During the roll:** a 1.14 worker reports no count, so its actors add
  nothing to the counter until their pool has rolled.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.15 to 1.16 step

1.16.0 stays on upstream v0.5.0-alpha2 and adds the `tcp` egress rule:
TCP that is neither TLS nor HTTP, allowed by hostname or CIDR and port.
Step to it from the latest 1.15 patch with the numbered steps above; the
step needs no migration, no snapshot reset and no manual step, and
conversations and durable volumes survive it.

- **Persisted format:** unchanged for every stored policy. `EgressRule`
  gains `tcp` (field 4 of its oneof), which no 1.15 policy sets; no CRD,
  no Postgres schema and no snapshot layout differs from 1.15.
- **Egress contract:** `v0.4.0-alpha1+path-prefix+tcp`. A 1.15
  `kubectl ate` and a 1.16 ate-api refuse each other's egress-policy
  writes; use the `kubectl ate` of the release the server runs.
- **Chart:** `images.agentgateway` is 2.5.0, which relays a stream a
  `tcp` rule allows, applies a path-prefixed credential to a path with a
  percent-encoded `/`, and answers a denied request with a 403 that names
  the destination and where it is allowed; the agentgateway egress config
  gains its TCP listener. The Envoy dataplane keeps closing such streams.
- **During the roll:** a `tcp` rule takes effect once the egress gateway
  runs the new agentgateway; until then such a stream is closed as before.
- **Credential path prefix:** a request path with a percent-encoded `/`
  (GitLab's `/api/v4/projects/<group>%2F<project>`) gets the
  `replace_headers` entry whose `path_prefix` it lies under, when reading
  `%2F` as a separator yields no dot or empty segment; until the egress
  gateway runs 1.16 it gets only the host-wide entry, as before.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### Patch steps within 1.16

Each patch step below needs no migration, no snapshot reset and no
manual step; no conversation and no durable volume is lost. A step from
1.15 straight to a 1.16 patch is the 1.15 to 1.16 step and the patch
entries in one roll.

#### 1.16.0 to 1.16.1

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs between the two tags.
- **Chart:** `images.agentgateway` is 2.6.0 (2.5.0 before), which counts
  each destination an actor's egress policy refuses on
  `agentgateway_atenet_egress_refusals_total` on the egress gateway's
  `stats` port, labeled by the actor (`ate_atespace`, `ate_actor_name`),
  `scheme`, `host` and `port`. The series are bounded: an actor's
  destinations beyond 16 read `host="other"`, all series beyond 4096 read
  `ate_actor_name="other"`, and a series idle for an hour is dropped. The
  `atenet-egress` PodMonitor already scrapes the port; no value changes.
- **During the roll:** refusals are counted once the egress gateway runs
  2.6.0.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.16.1 to 1.16.2

- **Persisted format:** unchanged. No proto field, no CRD, no Postgres
  schema and no snapshot layout differs between the two tags.
- **Chart:** `images.agentgateway` is 2.6.1 (2.6.0 before). An actor's
  stream that sends nothing before the egress gateway's protocol detection
  times out, or that closes first (a server-first protocol such as SSH, a
  connect-only probe), is decided by the actor's `tcp` rules: a refused one
  is counted on `agentgateway_atenet_egress_refusals_total` with
  `scheme="tcp"`, an allowed one is relayed. Before, such a stream was
  closed without a decision and nothing was counted. No value changes.
- **During the roll:** such streams are decided once the egress gateway
  runs 2.6.1.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

#### 1.16.2 to the next 1.16 patch

- **Persisted format:** unchanged.
- **Metrics:** `atenet_router_egress_refused_total` is gone from the
  egress gateway's `metrics` port. It lived in atenet's egress ext_proc
  handler, which the agentgateway data plane never calls, so it always
  read zero. Egress refusals are counted where agentgateway makes them,
  on `agentgateway_atenet_egress_refusals_total` on the `stats` port; an
  alert or dashboard on the atenet counter moves to it.
- **Migration:** none to run.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### The 1.16 to 1.17 step

1.17.0 stays on upstream v0.5.0-alpha2 and serves the actor JWT mint as
`Control.MintActorJwt`, the name agent-substrate/substrate `main`
declares (agent-substrate/substrate#2520). Step to it from the latest
1.16 patch with the numbered steps above; the step needs no migration
and no snapshot reset, and conversations and durable volumes survive
it.

- **Persisted format:** unchanged. No CRD, no Postgres schema and no
  snapshot layout differs from 1.16. `MintActorJWTRequest` and
  `MintActorJWTResponse` are `MintActorJwtRequest` and
  `MintActorJwtResponse`, with the same fields and numbers.
- **ate-api contract:** the gRPC method is
  `/ateapi.Control/MintActorJwt`. ate-api answers a call to
  `/ateapi.Control/MintActorJWT` with `Unimplemented`, so every caller
  of the mint moves with the server.
- **Chart:** `images.agentgateway` must be an agentgateway line release
  that mints through `MintActorJwt`. A release that still calls
  `MintActorJWT` denies every request with an `actor_jwt` header source
  with `500`.
- **During the roll:** while ate-api-server and the egress gateway run
  different releases, a request with an `actor_jwt` header source is
  denied with `500`; other egress rules are unaffected.
- **Goldens:** kept.
- **Reset:** none. **Conversations lost:** none.

### After the roll, on GKE

> [!WARNING]
> Only after every node shows `$NEW_VERSION` in [Check your
> progress](#check-your-progress). Relabeling the node pool applies to
> every node in it at once, with no drain and no pacing.

Relabel the node pool with `$NEW_VERSION` so nodes created later start
at the new version, then restore the autoscaler from the config you
saved [before the roll](#on-gke):

```bash
# --node-labels REPLACES the pool's full user label set: list the
# current labels first and carry them all over.
gcloud container node-pools describe $NODEPOOL --cluster $CLUSTER --zone $ZONE \
  --format='get(config.labels)' # copy the output

gcloud container node-pools update $NODEPOOL --cluster $CLUSTER --zone $ZONE \
  --node-labels=<every existing user label>,ate.dev/substrate-version=$NEW_VERSION

# restore the autoscaler config
gcloud container clusters update $CLUSTER --zone $ZONE --node-pool $NODEPOOL \
  --enable-autoscaling --min-nodes <min> --max-nodes <max>
```

## Something's wrong, or I need to undo

First run [Check your progress](#check-your-progress). It tells you
which step you reached, and everything below is keyed on that.

If a command failed, rerun it: every step is idempotent. The one
exception is drain. There is no undrain; if you drained the wrong
node, check that its workers are empty (suspending as needed), delete
their pods, and let the Deployment's replacements register as fresh
workers.

To roll back, undo what you did in reverse order: the control plane
first, then the nodes, then ate-controller last. The roll never edits
or deletes the old objects, so the old `WorkerPool` is intact, the pods
that lost their nodes stay Pending, and the old atelet DaemonSet is
still installed. Rolling nodes back is a matter of label flips. Run the
commands from the old release's checkout with the same environment as
the install, `VERSION` included if the install pinned it.

- Relabeled the GKE node pool: park the autoscaler again as
  [before the roll](#on-gke), then move the node pool label back to
  `$OLD_VERSION` with the [after the roll](#after-the-roll-on-gke)
  command.
- Past step 6: `go run ./cmd/ate-setup deploy apiserver` now, and
  `go run ./cmd/ate-setup deploy ate-system` once the nodes are back.
- Past step 5: roll each flipped node back by running step 5 with the
  sides swapped: drain, get every actor off the node as in b and c,
  flip the label back to `$OLD_VERSION`, and delete the node's
  new-pool pods.
- Past step 4: once no actor is assigned to a new-pool worker, delete
  each clone: `kubectl -n $NS delete workerpool $NEW_WORKERPOOL`.
- Past step 3: once no node carries `$NEW_VERSION`, delete the new
  DaemonSet: `kubectl delete daemonset -n ate-system -l app=atelet,ate.dev/substrate-version=$NEW_VERSION`.
- Past step 2: `go run ./cmd/ate-setup deploy ate-controller`.

`kubectl get ds -n ate-system -l app=atelet` must not show a third
DaemonSet afterwards. A third means the old checkout produced a
version other than `$OLD_VERSION`: delete it and check `VERSION`.

Abandoning the upgrade entirely is the whole list, top to bottom.
Retiring the old pool (below) is a separate, deliberate step. As long
as the old objects exist, rollback is one label flip per node.

## Retire the old pool

After the new version has soaked, reclaim the reserve. Save the old
pool's spec first: its `workerImage` ref is by digest and stays
pullable, so the saved file is the last-resort way to recreate the
pool. Then check the guards. Deleting the old objects is what ends
the rollback option.

```bash
kubectl -n $NS get workerpool $OLD_WORKERPOOL -o yaml > old-pool-backup.yaml

# Guards: no node still at $OLD_VERSION; no old-pool pod Running (Pending is
# expected); no actor assigned to an old-pool worker.
kubectl get nodes -l ate.dev/substrate-version=$OLD_VERSION
kubectl -n $NS get pods -l ate.dev/worker-pool=$OLD_WORKERPOOL
kubectl ate get workers

# Retire the old pool (its Deployment and pods go with it) and the
# old atelet DaemonSet.
kubectl -n $NS delete workerpool $OLD_WORKERPOOL
kubectl delete daemonset -n ate-system -l app=atelet,ate.dev/substrate-version=$OLD_VERSION
```

The new pool keeps its name. Names mean nothing to placement, so
`counter-v2` can serve indefinitely, and the next upgrade clones it
to `counter-v3`.
