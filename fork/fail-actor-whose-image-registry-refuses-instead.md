# Fail an actor whose image the registry refuses instead of resuming it forever

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/golden-boot-image-pull-terminal`](https://github.com/giantswarm/substrate/tree/upstream/golden-boot-image-pull-terminal) here (`4a56c0af`, the same commit on the mirror `main`; its message is the pull-request text). agent-substrate/substrate#1220 (merged 2026-09-23, after the v0.2.0-beta4 pin; the next re-pin decides whether this patch still applies or folds into it) crashes actors on every failure not marked retriable and classifies registry answers with `transport.Error.Temporary()`; its reviewer had asked for the status-based classification this patch does for the one class that is a registry's final word — the patch was written to apply on its own; the re-pin onto a release with #1220 checks whether it still does; agent-substrate/substrate#1817 (in v0.3.0-alpha1) then deleted the `ateerrors` taxonomy this patch classifies with, so the patch is reworked on alpha1's `ActorStatus.crash`. A team member opens the kagent-dev/substrate pull request with DCO sign-off once the team has reviewed it
- **Our commit:** `d14cac64`, [#14](https://github.com/giantswarm/substrate/pull/14)

## Change

Fail an actor whose image the registry refuses instead of resuming it forever (the image cache tags a registry's final word — a 4xx other than 408 or 429: manifest or repository unknown, unauthorized, denied — with `ReasonFailedGetExternalObject`; the Run/Restore boundaries claim it; `maybeCrashActor` returns the crash with its cause and the directive; the ActorTemplate reconciler fails the template with that cause when the resume reports the crash)

## Reason

a Harness whose `workload.image` could not be pulled never booted and was never reported: atelet's pull failed with the registry's answer, the Run RPC returned it untagged, ate-api retried the resume with backoff, `GoldenSnapshotStatus.ErrorMessage` stayed empty, kagent reported `Ready=False ActorTemplatePending` for as long as anyone waited and a worker stayed pinned to the golden actor (measured in giantswarm/agent's ATS on `v0.0.27-gs.2`: 300 s, no message). Now `Ready=False ActorTemplateFailed` with `GoldenActorCrashed: actor ate-golden/<uid> crashed: … MANIFEST_UNKNOWN: manifest unknown`, in seconds
