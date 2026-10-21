---
id: ateom-keeps-gvisor-sandbox-log
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(ateom): keep the gVisor sandbox''s own log in the actor''s log'
  - 'fix(ateom): give the gVisor sandbox''s boot process the debug log'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: [ateom-reports-workload-container-exit]
verified: 2026-10-21
---
# ateom keeps the gVisor sandbox's own log in the actor's log

## Change

ateom-gvisor's create of an actor's pause container, which boots the sandbox, passes runsc a log pipe as its first
extra file and points both `-log` and `-debug-log` at it (`/proc/self/fd/3`), with `-debug-command boot`. `-log`
carries runsc's fatal errors; the debug log is what runsc donates to the sandbox's boot process, and `-debug-command
boot` keeps it to that process, which keeps it until it exits (and dups it onto its stderr, so a sentry panic lands there
too). ateom writes every line it reads to the actor's log under the pause container's name
(`ate.actor.container.name: _pause`), as it does for an application container's output. The level stays runsc's
default (no `-debug`): a few lines per boot and restore, and what the sentry warns about. `-log` alone, the first
commit, reached only the create command's own log and fatal errors: runsc gives the boot process `--log-fd` as its
fatal-error writer and sends its log to the debug log or nowhere.

## Reason

Without a log file runsc connects the sandbox's own output to `/dev/null`, so the sentry's account of what it did to
the application was lost. In the kagent e2e on gVisor a restored claude actor loses one process, the harness or its
Claude Code child, to a SIGKILL about two seconds after `runsc restore -background`, with the sandbox still running,
no memory pressure and nothing in ateom or atelet sending it; the turn ends failed. The only path in the sentry that
ends a single process that way logs a warning first (`Unexpected SwitchToApp error`, the platform failing the switch
into the application), and that warning is what this log now carries, so the next occurrence names the error.

## History

not opened yet: the upstream-ready change is on the branch `upstream/ateom-sandbox-log` (`65b4a56d`, the pin
v0.5.0-alpha2, DCO-signed; there `StartJSONLogPipe` keeps no output tail).

[#525](https://github.com/giantswarm/substrate/pull/525)
