# A snapshot extract creates only `0:0` character devices

- **Status:** carried
- **Upstream:** [agent-substrate/substrate#2111](https://github.com/agent-substrate/substrate/pull/2111) (merged 2026-10-06, after the pin). Falls away at the re-pin onto the first release that carries it
- **Our commit:** `eb04dfc1` ([#260](https://github.com/giantswarm/substrate/pull/260), rebase-merged 2026-10-10; first release 1.8.0-rc.6): `git cherry-pick -x` of upstream's squash commit `8da76493`. Adapted to the line: it still mounts through `mount(8)`, so `nosuid,nodev` joins the overlay's `-o` options and `BindIntoShare` calls `setNosuidNodev` after the rbind; the hunk's `RemountReadOnly` and `Unmount` tests cover helpers the line does not have and are left out

## Change

A snapshot extract creates only `0:0` character devices (overlay whiteouts) and sets only `user.*` xattrs and the path-based overlay attributes (opaque, redirect, impure); `Create` skips other device nodes; on the micro-VM host the merged rootfs overlay and the binds into the virtio-fs share are `nosuid,nodev` (`setNosuidNodev`, recursive)

## Reason

snapshot tars are extracted as root on the host, and the extract made any device node and set any `trusted.overlay.*` xattr the archive named; a stray device could make a snapshot unrestorable (upstream's agent-substrate/substrate#2090)
