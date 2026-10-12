# The snapshot takes one rootfs-upper tar per container and one tar per durable volume; the restore …

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2110](https://github.com/agent-substrate/substrate/pull/2110) (merged 2026-10-06, after the pin). Falls away at the re-pin onto the first release that carries it
- **Our commit:** `3afd756f` ([#260](https://github.com/giantswarm/substrate/pull/260), rebase-merged 2026-10-10; first release 1.8.0-rc.6): `git cherry-pick -x` of upstream's squash commit `893b1edb`, clean

## Change

The snapshot takes one rootfs-upper tar per container and one tar per durable volume; the restore creates the layout before it extracts into it, so a snapshot no longer decides which host paths are mounted or deleted; `tarutil` writes and reads the tree through an `os.Root` (`fs.WalkDir` over `root.FS()`, `root.Lstat`, `root.Readlink`), component by component, never by the full host path (`internal/tarutil`, `cmd/ateom-gvisor/durable.go`, `cmd/ateom-microvm/rootfsupper.go`)

## Reason

a workspace tree deeper than `PATH_MAX` crashed the actor at its checkpoint and lost the session on every harness: the tar walked and stat'ed each entry by its full path, which the kernel refuses with `ENAMETOOLONG`; the same tar named the host paths a restore mounted and deleted (upstream's agent-substrate/substrate#2083). **Snapshots written before it do not resume after it on their own**, nor its own on a release before it: since 1.8.1 the forward step migrates them (the conversion's and the golden re-take's rows below), with no snapshot reset, and the step's floor is 1.8.1 (`docs/upgrade.md`, "The 1.7 to 1.8 step"); `docs/snapshot-reset.md` stays the remedy for a corrupted snapshot store only
