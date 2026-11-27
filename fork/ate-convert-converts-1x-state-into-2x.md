---
id: ate-convert-converts-1x-state-into-2x
fork: substrate
theme: compat-migrations
category: compat
status: carried
commits:
  - 'feat(ate-convert): convert the 1.x PostgreSQL rows into the 2.x schema'
  - 'feat(ateworker-gvisor): convert durable-dir tars into an fs bundle'
  - 'feat(ate-convert): convert snapshots and the ate.dev CRDs'
upstream:
  outcome: fork-only
  refs: [agent-substrate/substrate#3433]
  exit: "drops at the first re-pin after every installation ran it on 2.0.x; upstream's answer to pre-squash databases is to recreate them (docs/dev/postgresql-schema-evolution.md), so the exit is the fleet, not an upstream change"
depends_on: [existing-volume-mounted-per-actor-at-sub, pause-suspend-resume-take-optional-fencingtoken]
verified: 2026-11-27
---
# ate-convert moves a 1.x installation's state onto 2.x

## Change

`cmd/ate-convert` reads the 1.x rows in schema `public` and writes the 2.x rows into schema `substrate`, which the 2.x
installer's `postgressetup` creates: every bytea row re-encoded (the #2888 renumbering, PAUSED to SUSPENDED on its newest
durable copy, `external_snapshot` to one `Snapshot{owner ACTOR or TAG, fidelity VOLUMES, durable COMPLETED}` at the same
URI, `sandbox_assets` dropped, template `on_commit` FULL/DATA to `preferred_fidelity` MEMORY/VOLUMES), with the 1.x
read-migrations folded in; goldens cleared so 2.x takes them again; no worker rows, assignments or leases; a goose
ledger at versions {0,1}. `public` is never written. Snapshots are converted in place, option (a) of the conversion
design: the fork's durable tars are restored into a scratch gVisor sandbox with the template's container names and
mount paths and checkpointed with 2.x's `runsc fscheckpoint` (a hidden `fsconvert` subcommand of `ateworker-gvisor`,
built only with `-tags ateconvert`), trial-restored and compared with the tars, then `fs/*.zstd` and a 2.x
`manifest.json` are written beside the kept fork objects and `manifest.fork.json`. `crds prepare|restore` moves
WorkerPool, SandboxConfig and CSIDriverConfig to v1 storage. `--dry-run`, `--verify` (counts, per-actor hashes, trial
restores) and reruns converge. The 1.x protos are a frozen regenerated copy under `cmd/ateapi/forkconvert/internal/forkpb`
(proto package `forkateapi`).

## Reason

v1.0.1 reads none of the line's 1.x state: unconverted rows flip meaning (PAUSED reads as DELETING, FULL as VOLUMES),
and a VOLUMES restore without an fs bundle boots empty volumes. Customers hold sessions, workspaces and durable
directories in that state.

## History

2026-11-27: added at the re-pin onto kagent-dev/substrate v1.0.1 (`release-1.0@7ce6f439`). The runsc path is proven
only by a lab spike of one single-container durable snapshot (converted offline with 2.x's runsc and restored with
every entry intact); the converter's own round trip runs in agentlab before any installation.
