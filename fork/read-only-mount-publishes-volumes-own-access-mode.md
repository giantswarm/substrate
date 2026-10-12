# A read-only mount is published with the volume's own access mode

- **Status:** carried
- **Upstream:** amends the existing-volume patch (`existing-volume-mounted-per-actor-at-sub.md`) and follows agent-substrate/substrate#1637 (open) in its shape. Exit: drops with that patch at the re-pin that carries #1637's API
- **Our commit:** [#342](https://github.com/giantswarm/substrate/pull/342) (`fix(volume): publish a read-only mount with the volume's own mode`)

## Change

ate-api sends atelet an existing volume's PersistentVolume's own access mode — the first in the PersistentVolume's order that permits the actor's — and `ExternalVolumeSource.read_only` (internal field 5) for a `READ_ONLY_MANY` volume; attach uses the same mode. The CSI plugin's `MountVolume` takes the read-only flag: it stages and publishes with the volume's mode and `NodePublishVolumeRequest.readonly` set for a read-only mount or a `READ_ONLY_MANY` mode, as the kubelet does.

## Reason

A read-only mount asked for `MULTI_NODE_READER_ONLY` whatever the PersistentVolume permits; the AWS EFS CSI driver supports only the PersistentVolume's own mode for a `ReadWriteMany` volume and refused the publish (`invalid access mode: MULTI_NODE_READER_ONLY`), so an actor with read-only mirrors on EFS never resumed ([#340](https://github.com/giantswarm/substrate/issues/340))
