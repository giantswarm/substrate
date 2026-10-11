# Snapshot reset runbook

This runbook resets every snapshot kagent holds on an installation —
golden and session — and lets every Agent take its golden again. It is
the remedy for a corrupted snapshot store only: a store whose snapshots
the running release can neither restore nor convert, so that every
resume of a session and every create from a golden fails. No release
step on this line needs it, and every conversation ends with it.

**The step from 1.7 to 1.8.1 needs no reset and no manual step.** 1.8.0
changed the snapshot layout (the PATH_MAX-safe snapshot tars: one
rootfs-upper tar per container and one tar per durable volume, the
restore creating the layout before it extracts; FORK.md names the
carried commits), and 1.8.1 migrates the snapshots written before it on
its own: each actor's durable snapshot in the 1.7 layout is converted on
its first resume, and every golden written in an older layout is taken
again on the new workers (see below and the 1.7 to 1.8 note of the
[rolling upgrade runbook](upgrade.md#the-17-to-18-step)). Conversations
and durable volumes survive the step. 1.8.0 itself is no step target:
it refuses to restore a 1.7 durable snapshot.

A roll after which runsc refuses the checkpoints it finds leaves such a
store too. runsc refuses to restore a checkpoint into a container whose
user or working directory differ from the checkpoint's; the line's
release that made actors run as their image's `USER` in its `WORKDIR`
needed every gVisor template recreated after it. A Harness image pinned
by digest never changes under a revision — a new digest is a new
revision with a new golden, and sessions move onto it data-only — so a
`USER` or `WORKDIR` change in a Harness image needs no reset by itself.
An image re-pushed under a mutable reference with a changed `USER` or
`WORKDIR` hits the refusal on every template whose golden predates it;
the reset is the remedy.

## The 1.7 to 1.8 step, and how a missed migration shows

Rehearsed in agentlab between 1.7.0 and 1.8.0-rc.6, on the Go ADK and the
claude Harness, before the migration existed:

- **Forward (1.7.0 snapshots on 1.8.0-rc.6):** the restore reported no
  error. The actor's memory came back, its durable directory did not —
  the new reader found no per-volume tar in the old snapshot and restored
  nothing — and the harness ran on an empty `/data`. The failure showed
  at the workload: the Go ADK failed its first turn with `failed to fetch
  app state: SQL logic error: no such table: app_states`, the claude
  harness with `Harness runtime execution failed`. The Agent stayed
  `Ready`, every new session on its golden failed at once, and the
  checkpoint after the failed turn uploaded an empty durable tar. 1.8.0
  refuses such a restore with `FailedPrecondition` instead
  ([#265](https://github.com/giantswarm/substrate/pull/265)): the actor
  crashes rather than run on an empty `/data`.
- **Backward (1.8 snapshots on 1.7.0):** the restore fails and the actor
  crashes: `while restoring durable-dir volumes … opening tar
  "…/restore-state/durable-dir.tar": no such file or directory`. A turn
  answers `runtime lost: Actor … crashed; start a new conversation`. The
  line steps forward only; a store left behind by a forced roll back
  below 1.8 is one the running release cannot read, and the reset is its
  remedy.

Since 1.8.1 the forward step migrates on its own:

- **Session snapshots:** a gVisor ateom converts a durable snapshot in
  the 1.7 layout on the actor's first resume — `durable-dir.tar` is split
  into one `durable-dir-<volume>.tar` per declared volume, tar to tar,
  all or nothing, and each volume is restored like a current snapshot;
  the next checkpoint writes the per-volume layout
  ([#277](https://github.com/giantswarm/substrate/pull/277)). A snapshot
  that does not convert — an unreadable archive, an entry outside every
  declared volume, a hardlink across volumes — is refused with
  `FailedPrecondition` and the cause, never restored with empty volumes.
- **Golden snapshots:** once the new ate-api-server serves, the
  ActorTemplate reconciler takes every golden recorded in an older
  layout, or without one, again on the new workers in the current layout
  and then deletes the old one
  ([#282](https://github.com/giantswarm/substrate/pull/282)). Until a
  template's new golden is complete, a create on the template answers
  `Unavailable` and succeeds on retry.

What a reset covers:

- **Golden snapshots** — one per ActorTemplate, the boot source of every
  new session of that Agent revision.
- **Session snapshots** — kagent suspends a session's actor after each
  turn (a DATA snapshot: the durable directory) and the next turn
  restores the golden plus that data. The conversation's files are gone
  to the session.
- **Node-local checkpoints** of paused sessions live on the worker that
  took them. The WorkerPool's worker image follows the chart's Substrate
  pin, so the roll replaces every worker and these are gone with it.

kagent re-takes no golden on a Substrate roll: it keys an ActorTemplate by
the Agent's revision — its UID, Harness image and environment, template —
never by the Substrate release. Substrate's ActorTemplate reconciler does,
since 1.8.1: a golden whose recorded snapshot layout is older than the one
the workers write, or unrecorded, is taken again once the release has
rolled, and a golden in the current layout is restored as its tag names
it. A corrupted golden in the current layout is not taken again; the
reset below takes it.

## The reset

Precondition: the roll is complete. Every pod in `ate-system`, the atelet
DaemonSet and the WorkerPool's worker image run the target release, and
the pool is healthy:

```bash
kubectl -n ate-system get pods -o custom-columns='NAME:.metadata.name,IMAGE:.spec.containers[0].image'
kubectl -n kagent get workerpool -o custom-columns='NAME:.metadata.name,IMAGE:.spec.workerImage,DESIRED:.spec.replicas,READY:.status.readyReplicas'
```

1. **Announce.** Every conversation ends with the reset; people start new
   ones afterwards. Nothing in a session survives: that is what the
   release changed.

2. **Hold the Agents' re-creation.** Flux reconciles each Agent's
   HelmRelease with drift detection and would re-create a deleted Agent
   before its sessions are swept — and a session whose Agent is back under
   the same name keeps its revision and its snapshot. Suspend the agent
   HelmReleases first (they live in the agents' namespace; the platform's
   own HelmReleases are elsewhere and stay as they are):

   ```bash
   flux suspend helmrelease -n kagent --all
   ```

3. **Delete every Agent.**

   ```bash
   kubectl delete agents.api.kagent.dev -A --all
   ```

   kagent retires the Agents' definitions. Its expiration worker deletes
   every session of a deleted Agent on its next sweep
   (`KAGENT_SESSION_EXPIRATION_POLL_INTERVAL`, one minute), and Substrate
   deletes each session's actor with its snapshot prefix in the store. Its
   runtime-revision garbage collector then deletes every revision nothing
   references (`KAGENT_RUNTIME_REVISION_GC_INTERVAL`, one minute), and
   Substrate deletes the golden actor, the golden tag and the tag's objects
   with each ActorTemplate.

4. **Verify Substrate and the store are empty**, about three minutes
   later. Through the Control API (the port-forward, CA and token of the
   [rolling upgrade runbook](upgrade.md#before-you-start), the proto from
   this repository):

   ```bash
   for rpc in ListActorTemplates ListTags ListActors; do
     grpcurl -cacert /tmp/ate-ca.pem -authority api.ate-system.svc \
       -import-path pkg/proto/ateapipb -proto ateapi.proto \
       -H "authorization: Bearer ${TOKEN}" 127.0.0.1:8443 "ateapi.Control/${rpc}"
   done   # each answers {}
   ```

   The snapshot store holds no object under any `<location>/atespaces/`
   prefix. In kagent's database `runtime_revision` has no row and
   `agent_definition` no row with `retired_at IS NULL`.

5. **Resume.** Each Agent comes back with a new UID, which is a new
   revision: a new ActorTemplate, its golden boot on the new Substrate.

   ```bash
   flux resume helmrelease -n kagent --all
   kubectl get agents.api.kagent.dev -A   # every row Ready
   ```

   An Agent that was applied by hand is applied again.

6. **Prove** one turn per Harness, then a suspend and a restore of that
   session (in agentlab: `agentlab turn --template <t> --harness <h>
   --keep …`, then `--session <id> --suspend --keep …`).

**Rollback** is the same procedure after the pin has been rolled back: the
snapshots the newer release wrote are as useless to the older one.

## What does not work instead

- *Re-taking the goldens in place* — deleting the ActorTemplates through
  the Control API and restarting the kagent controller, which re-creates
  each template under its name with a fresh golden. Every session keeps
  its data snapshot and fails as before until it is deleted.
- *Re-compiling every revision with a Harness change* (an environment
  variable). New goldens, but the sessions on the superseded revisions
  keep their snapshots and are repointed data-only onto the new golden —
  data the new Substrate cannot read.
- *Purging the bucket.* Substrate keeps the records of tags and actors
  whose objects are gone, and every restore then fails for good instead
  of silently.
