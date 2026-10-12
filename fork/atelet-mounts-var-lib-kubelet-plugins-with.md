# atelet mounts `/var/lib/kubelet/plugins` with `mountPropagation

- **Status:** carried
- **Upstream:** [kagent-dev/substrate#39](https://github.com/kagent-dev/substrate/pull/39) — opened 2026-09-15 from `upstream/atelet-plugins-mount-propagation` (the chart hunk on upstream `main`; the chart test stays fork-only, upstream has no chart tests)
- **Our commit:** `b79ed513`

## Change

atelet mounts `/var/lib/kubelet/plugins` with `mountPropagation: HostToContainer`

## Reason

with the default (private) propagation atelet's mount namespace kept a copy of every CSI globalmount on the node; after a pod moved, the volume's filesystem (and LUKS mapper) stayed open there and Longhorn's `NodeUnstageVolume` failed forever with `luksClose: Device is still in use` — the SPIRE outage of 2026-09-11 on the homelab cluster
