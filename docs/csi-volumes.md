# CSI Volumes for Actors in Agent Substrate

Substrate integrates with the **Container Storage Interface (CSI)** to provide dynamically provisioned, per-actor external volumes that seamlessly attach and detach as actors transition through their lifecycle.

---

## 1. CSI in Substrate vs. Standard Kubernetes

In Kubernetes, volumes are reconciled asynchronously via standard Kubernetes objects (e.g. `PersistentVolumeClaim`, `PersistentVolume`). Agent Substrate takes a different approach tailored for actor lifecycle operations:

* **No PV or PVC Objects:** External volumes are declaratively defined in the [`ActorTemplate`](api-guide.md#2-actortemplate-the-workload-blueprint) via `externalVolumeTemplate` and provisioned dynamically for each actor instance. Volume operations are coupled directly with the actor lifecycle.
* **Direct Network-Based CSI Controller:** The Substrate control plane (`ateapi`) communicates directly with the CSI Controller gRPC service over the network (via TCP or DNS endpoints, optionally secured with TLS/mTLS).

---

## 2. Dynamic CSI Driver Discovery (`CSIDriverConfig`)

To discover and communicate with CSI drivers, Substrate uses dynamic discovery driven by the cluster-scoped **`CSIDriverConfig`** Custom Resource Definition (CRD).

### The `CSIDriverConfig` Resource

`CSIDriverConfig` defines the gRPC connection parameters for a specific CSI driver. It bridges the Kubernetes `StorageClass` (referenced in the `ActorTemplate`) to the network endpoint of the CSI Controller service and the local socket path of the CSI Node plugin.

```yaml
apiVersion: ate.dev/v1alpha1
kind: CSIDriverConfig
metadata:
  name: nfs.csi.k8s.io
spec:
  driverName: nfs.csi.k8s.io
  controllerEndpoint: tcp://csi-nfs-controller.kube-system.svc.cluster.local:50052
  nodeSocketOverride: unix:///var/lib/kubelet/plugins/csi-nfsplugin/csi.sock
  tls:
    enabled: true
    usePodIdentity: true
    serverName: csi-nfs-controller.kube-system.svc.cluster.local
```

### Specification (`CSIDriverConfigSpec`)

| Field | Type | Description |
| :--- | :--- | :--- |
| `driverName` | `string` | **Required.** The standard CSI driver name (e.g. `nfs.csi.k8s.io`, `hostpath.csi.k8s.io`, `pd.csi.storage.gke.io`). Matches the `provisioner` field on the referenced Kubernetes `StorageClass`. |
| `controllerEndpoint` | `string` | **Required.** The gRPC endpoint for the CSI Controller service. Must be a valid URI starting with `tcp://`, `dns:///`, or `unix://` (e.g., `tcp://csi-controller.kube-system.svc:50051` or `dns:///csi-svc.default.svc:9000`). |
| `nodeSocketOverride` | `string` | **Optional.** Override for the CSI Node service Unix domain socket on worker nodes. Must begin with `unix://`. If omitted, Substrate defaults to `unix:///var/lib/kubelet/plugins/<driverName>/csi.sock`. |
| `tls` | `*CSIDriverTLSConfig` | **Optional.** Configures TLS or mTLS for the gRPC connection to the `controllerEndpoint`. |

#### TLS / mTLS Configuration (`spec.tls`)

| Field | Type | Description |
| :--- | :--- | :--- |
| `enabled` | `bool` | **Required.** Enables TLS/mTLS for the gRPC connection. |
| `usePodIdentity` | `bool` | **Optional.** When `true`, reuses Substrate's SPIFFE Pod Identity certificates for mutual TLS (mTLS) with dynamic CA trust bundle verification and rotation. Must be `true` when `enabled` is `true`. |
| `serverName` | `string` | **Optional.** Server name override for TLS certificate verification. |

> [!NOTE]
> For details on exposing CSI controller endpoints over the network and configuring CSI node DaemonSets with required mount propagations, see the [CSI Driver Deployment Guide](csi-deployment.md).

---

## 3. ActorTemplate: Configuring CSI Volumes

External volumes are declared on the `ActorTemplate` resource. For complete details on actor templates, see the [ActorTemplate: The Workload Blueprint](api-guide.md#2-actortemplate-the-workload-blueprint) section in the Substrate API Guide.

### Volume Configuration Fields

To attach a CSI volume to an actor:

1. Define the volume under `volumes` with an `externalVolumeTemplate`.
2. Mount the volume inside one or more containers under `containers[].volumeMounts`.

#### `volumes[]`

```yaml
volumes:
- name: my-data-volume
  externalVolumeTemplate:
    capacity: 10Gi
    storageClassName: standard-rwx
```

* `name`: Unique DNS-label-compliant volume name.
* `externalVolumeTemplate.capacity`: Quantity string representing the requested volume size (e.g. `1Gi`, `50Gi`).
* `externalVolumeTemplate.storageClassName`: Name of a Kubernetes `StorageClass` present in the cluster whose `provisioner` matches a registered `CSIDriverConfig`.

#### `containers[].volumeMounts[]`

```yaml
volumeMounts:
- name: my-data-volume
  mountPath: /var/data
```

* `name`: Must match the declared `volumes[].name`.
* `mountPath`: Unix path inside the container sandbox where the volume will be mounted.

> [!NOTE]
> All declared volumes in `volumes` must be mounted by at least one container.

---

### Existing Volumes: One Volume, Many Actors

A volume that exists outside Substrate and outlives its actors, such as a read-write-many workspace several actors work on, is an **existing volume**. The template declares it by name and mounts it; each actor supplies it at `CreateActor`:

```yaml
volumes:
- name: workspace
  existingVolume: {}
- name: mirrors
  existingVolume: {}
containers:
- name: agent
  volumeMounts:
  - name: workspace
    mountPath: /workspace
  - name: mirrors
    mountPath: /mirrors
    readOnly: true
```

```yaml
existingVolumes:
- name: workspace
  driver: nfs.csi.k8s.io
  volumeHandle: <the PersistentVolume's spec.csi.volumeHandle>
  accessMode: VOLUME_ACCESS_MODE_READ_WRITE_MANY
  subPath: sessions/a
- name: mirrors
  driver: nfs.csi.k8s.io
  volumeHandle: <the same handle>
  accessMode: VOLUME_ACCESS_MODE_READ_ONLY_MANY
  subPath: mirrors
```

* An existing volume's `subPath` is the directory this actor sees as the volume's root; a mount's own `subPath` is relative to it. So one template serves every actor, each in a directory of its own. One volume may be mounted at several paths.
* The directory is resolved beneath the volume's root without following any symbolic link, so a link that another actor wrote on the volume fails the mount rather than redirecting it. A read-write mount's directory is created when its last component is missing, mode `0770` and owned by the user the mounting container runs as, so a caller may name a directory of its own for the actor in the create request; its parent must exist. A read-only mount's directory must exist, and so must every directory of a `READ_ONLY_MANY` volume.
* `readOnly` mounts read-only, and so does every mount of a `READ_ONLY_MANY` volume.
* `CreateActor` refuses a reference that names no existing volume of the template, a driver without a `CSIDriverConfig`, a handle that no PersistentVolume of the driver holds, and an access mode the PersistentVolume does not permit. The PersistentVolume's `spec.csi.volumeAttributes` are passed to the driver when the volume is mounted, so it must still exist when the actor resumes.
* An existing volume of the template that the actor does not supply contributes neither the volume nor its mounts.
* Substrate never creates, deletes or changes an existing volume, and never detaches one from a node, where other actors may be using it. Pause, resume (on another node too) and delete only unmount and mount it.
* An actor with existing volumes boots from its image instead of the template's golden snapshot, which was captured without their mounts, and cannot be created from a tag. `existingVolumes` is immutable.

---

## 4. End-to-End Example

The following example demonstrates setting up an NFS CSI driver with Substrate and deploying an `ActorTemplate` that mounts an external NFS volume.

### Step 1: Create the StorageClass

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-nfs-sc
provisioner: nfs.csi.k8s.io
parameters:
  server: nfs-server.default.svc.cluster.local
  share: /
reclaimPolicy: Delete
volumeBindingMode: Immediate
mountOptions:
  - nfsvers=4.1
```

### Step 2: Register the CSIDriverConfig

```yaml
apiVersion: ate.dev/v1alpha1
kind: CSIDriverConfig
metadata:
  name: nfs.csi.k8s.io
spec:
  driverName: nfs.csi.k8s.io
  controllerEndpoint: tcp://csi-nfs-controller.kube-system.svc.cluster.local:50052
  nodeSocketOverride: unix:///var/lib/kubelet/plugins/csi-nfsplugin/csi.sock
  tls:
    enabled: true
    usePodIdentity: true
    serverName: csi-nfs-controller.kube-system.svc.cluster.local
```

### Step 3: Define WorkerPool and ActorTemplate

Refer to [ActorTemplate: The Workload Blueprint](api-guide.md#2-actortemplate-the-workload-blueprint) for general template options.

The `WorkerPool` is a Kubernetes resource, applied with `kubectl apply`:

```yaml
apiVersion: ate.dev/v1alpha1
kind: WorkerPool
metadata:
  name: agent-pool
  namespace: ate-demo
  labels:
    workload: stateful-agent
spec:
  replicas: 5
  workerImage: ko://github.com/agent-substrate/substrate/cmd/ateom-gvisor
```

The `ActorTemplate` is a protojson-shaped `ateapipb.ActorTemplate`, created
through the ate API with `kubectl ate create actor-template -f -` (the
`ate-demo` atespace must exist):

```yaml
metadata:
  atespace: ate-demo
  name: stateful-agent-template
workerSelector:
  matchLabels:
    workload: stateful-agent
containers:
- name: agent
  image: gcr.io/my-project/agent-app@sha256:7f28ab0...
  volumeMounts:
  - name: shared-storage
    mountPath: /mnt/shared
  wakeupProbe:
    httpGet:
      path: /readyz
      port: 8080
sandboxConfig:
  sandboxClass: SANDBOX_CLASS_GVISOR
  configName: gvisor-default
snapshotConfig:
  storageLocation: gs://my-snapshots-bucket/stateful-agent
volumes:
- name: shared-storage
  externalVolumeTemplate:
    capacity: 5Gi
    storageClassName: csi-nfs-sc
```
