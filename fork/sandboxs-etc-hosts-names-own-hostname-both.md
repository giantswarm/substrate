---
id: sandboxs-etc-hosts-names-own-hostname-both
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateom): name the actor''s hostname in its /etc/hosts'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-19
---
# A sandbox's `/etc/hosts` names its own hostname

## Change

A sandbox's `/etc/hosts` names its own hostname: both ateoms write `127.0.0.1 localhost`, `::1 localhost ip6-localhost ip6-loopback` and `127.0.1.1 actor` (`dns.WriteRootfsHosts`, `ocispec.Hostname`) into each application container's bundle rootfs on run and restore — the micro-VM ateom beside the guest's resolv.conf, the gVisor ateom into the bundle's private upper rather than as a bind like resolv.conf, because runsc restore refuses a mount set that differs from the checkpointed one (`--restore-spec-validation` enforces by default) and a new mount would fail every existing snapshot. Tests: `TestWriteRootfsHosts`, `TestWriteRootfsHostsDoesNotFollowAPlantedSymlink`; the identity e2e asserts the entry, that the hostname resolves to 127.0.1.1 after a golden restore and a suspend/resume, and that neither actor's worker logged a DNS relay warning for `actor.`

## Reason

the hostname `actor` was in no sandbox's `/etc/hosts`, so every lookup of it (`hostname -f`, a runtime resolving itself at start) left the sandbox through the DNS relay; cluster DNS forwarded the bare name upstream, which answered SERVFAIL, and each e2e lane run logged 96 relay warnings for `actor.` ([#220](https://github.com/giantswarm/substrate/issues/220))

## History

to file: agent-substrate/substrate `main` (`fd7a9cde`, 2026-10-10) writes no `/etc/hosts` into the sandbox

`giantswarm`: [#243](https://github.com/giantswarm/substrate/pull/243)
