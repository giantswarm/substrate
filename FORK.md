# The Giant Swarm line of Agent Substrate

This repository is Team Bumblebee's fork of [kagent-dev/substrate](https://github.com/kagent-dev/substrate)
(itself a fork of [agent-substrate/substrate](https://github.com/agent-substrate/substrate)). The Giant Swarm
Agent Platform's kagent (API v2) runs every agent as a Substrate actor; this fork is where the platform's
Substrate is pinned, built, scanned and published. It answers one question from one place: **which Substrate
are we running, and why does it differ from upstream?**

Tracking: this fork's own work in [its issues](https://github.com/giantswarm/substrate/issues) (both upstream
repositories have issues disabled); the line
[giantswarm/giantswarm#37757](https://github.com/giantswarm/giantswarm/issues/37757), upstream engagement
[giantswarm/giantswarm#37742](https://github.com/giantswarm/giantswarm/issues/37742) (Substrate rows), the
kagent line built the same way [giantswarm/giantswarm#37010](https://github.com/giantswarm/giantswarm/issues/37010)
(`giantswarm/kagent-upstream`), the epic [giantswarm/giantswarm#37705](https://github.com/giantswarm/giantswarm/issues/37705).

## Branches

| Branch | What it is | Who moves it |
|---|---|---|
| `main` | A pure mirror of upstream `main`. Upstream rebases its `main` onto agent-substrate (no release tag is an ancestor of it), so the mirror is a **forced** update. Never edited, never the target of a pull request. The mirrored commits carry upstream's workflow files, whose `main` triggers would run upstream's suites here for nothing — the sync cancels those runs right after the push. | the sync workflow (as the HeraldBot App) — the `protect-main` ruleset admits the App and nobody else; a repair is a `workflow_dispatch` of the sync |
| `giantswarm` (default) | **The line**: the upstream release tag the platform's kagent pins ("the pin") + cherry-picked upstream fixes + this fork's own files. Every change of the fork's own files is a pull request against it. Merge method: a cherry-pick of an upstream commit is **rebase-merged**, one commit per patch, so its patch identity survives and `git rebase` drops it by itself once the pin contains it; fork-infrastructure pull requests are squashed. Edits of this file alone (`docs(fork)`) are merged the same way between re-pins, and a re-pin folds every one of them into the single ledger commit at the top of the replayed stack, so the line carries one commit per change and not one per ledger edit (since 2026-10-05; the pre-fold history stays reachable from the release tags). | pull requests (`run-tests` and `govulncheck` required); a re-pin lands only as a candidate's head those checks passed on, pushed by the sync workflow's `land` (the HeraldBot App) — never through a merge or `devctl pr merge`, never by hand |
| `fork/<topic>` | pull-request branches against `giantswarm` | anyone in the team |
| `release-1.0` | **The 1.0 line** for installations whose kagent speaks the v0.0.29 ate-api contract (kagent-upstream 1.0.x): `v1.0.1` plus what a 1.0.x patch needs, cut when `giantswarm` has already moved to a newer pin (it moved to v0.2.0-beta4 on 2026-09-22, a contract change the kagent line moves with). Pull requests against it are rebase-merged like the line's; its releases are `vX.Y.Z` tags on it, published by the same pipeline. Its ledger is this file on `giantswarm`; the branch's own copy carries the same rows for the tagged tree. Retired once the fleet's kagent is past the v0.0.29 contract. | pull requests; the same suites as `giantswarm` run on a pull request against it |
| `release-1.3` | **The 1.3 line** for installations on the meta chart's Substrate range `>=1.3.0 <1.4.0` once `giantswarm` moved to the v0.3.0-alpha3 pin (1.4.0): `v1.3.0` plus what a 1.3.x patch needs. Pull requests against it are rebase-merged; its releases are `v1.3.Z` tags on it, published by the same pipeline. Its ledger is this file on `giantswarm`; the branch's own copy carries the same rows for the tagged tree. Retired once the fleet runs 1.4. | pull requests |
| `sync/<date>-<pin>` | re-pin candidates (`sync/<date>-<line>-<pin>` for a maintenance line): the new pin + the replayed carried patches, each with a pull request against its line that is reviewed and closed by the landing, never merged; the landing deletes the branch, and the next re-pin's sync deletes every candidate it supersedes (any `sync/*` branch no other line's open pull request holds) | the sync workflow; a human finishes one whose replay conflicted |

## Pin

| | |
|---|---|
| Upstream tag | **v0.4.0-alpha1** (2026-10-04; tag commit `c2e2e130`, "Pass the fork workflow deadline in access policy tests", 42 commits on top of upstream `main` `16b863a0` of 2026-10-03, so not on `main`; it descends from v0.3.0 (`ccecc788`) but not from v0.3.0-alpha3, which was tagged off a side branch too, and the re-pin replays every carried commit). Previous pins: v0.0.26 (2026-09-10 → 2026-09-14, releases `v0.0.27-gs.1`–`gs.9`), v0.0.29 (2026-09-14 → 2026-09-22, releases `v0.0.30-gs.1`–`gs.5`, `1.0.0`, `1.0.1`; `release-1.0` stays on it), v0.2.0-beta4 (2026-09-22 → 2026-09-24, dev builds only), v0.2.0-beta5 (2026-09-24 → 2026-10-05, releases `1.1.0`–`1.3.0` and the `1.4.0` candidates; `1.4.0` is cut from `81abb52e`, the last candidate before the re-pin), v0.3.0-alpha3 (2026-10-05 → 2026-10-08, releases `1.4.0`–`1.5.x`) |
| Why this one | upstream kagent-dev/kagent pins `github.com/kagent-dev/substrate v0.4.0-alpha1` in `go/go.mod` (the `replace` of `github.com/agent-substrate/substrate`) since kagent-dev/kagent#3064 (`b8d8b5ca`, "chore(deps): upgrade Substrate to v0.4.0-alpha1"), and at kagent v1.0.0-alpha9 (`6c4cfb2b`, 2026-10-07), the tag the kagent line re-pins onto; `giantswarm/kagent-upstream` replaces the same path with this line's release. v0.4.0-alpha1 changes the ate-api contract kagent compiles against: the AccessPolicy RPCs (global and per atespace), `ActorJWTSource` among the credential sources, a Worker's and a sandbox's addresses repeated for dual-stack (`ips`, `worker_pod_ips`), trust bundles named as a list (`TrustBundleDataSource.names`), and `onResume.fromData` with the `DATA_ON_GOLDEN` restore scope removed. The PostgreSQL schema gains the access-policy tables (one additive migration, upstream's `000003_access_policies.sql`, unchanged on this line since its own migrations have their own ledger), so the move upgrades a 1.5.x database in place. The ate-api gRPC contract between kagent's client and Substrate's server must match, so the two lines move together (2026-09-22 onto beta4, 2026-09-24 onto beta5, 2026-10-05 onto alpha3, 2026-10-08 onto v0.4.0-alpha1). |
| agentgateway it runs | the `agentgateway-image` parameter of `.circleci/config.yml`, equal to the chart default `images.agentgateway` (the publish refuses a drift — the chart must install unstamped, and its egress config is written for this build's schema) and to the image, by digest, of `manifests/ate-install/components/agentgateway`, which the e2e installs (`hack/verify/agentgateway-image.sh` fails a pull request on any drift; before the 2026-10-08 re-pin that component ran upstream's alpha build, so the e2e never ran the line's agentgateway): a release of the agentgateway line, published by that line from CircleCI under its own gsoci path and versioned on its own — **`gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0`** (tag `v2.3.0` on `97b142d6` = the agentgateway line's 2026-10-05 re-pin onto upstream v1.6.0 (`ea560864`) plus its 40 carried commits; v1.6.0 contains agentgateway#3677 (`8d6be5fb`), the egress gateway authorizing from the `ateom-for-actor` SPIFFE URI of agent-substrate/substrate#1809 that alpha3's ateom presents and 2.2.2 refuses, and agentgateway#3689 (`c4143926`), the squash of `50999825`, the build upstream's v0.4.0-alpha1 chart pins as `ghcr.io/kagent-dev/substrate/agentgateway:50999825cb55` (alpha1 commit `50e20d5e`), so the v0.4.0-alpha1 re-pin needs no new agentgateway release; pinned here by `a07cc154`; before it 2.2.2, tag `v2.2.2` on `149d9b40` = 2.2.1 + an egress policy call that never reached its policy service answered `503 unavailable` instead of `403 denied`, giantswarm/agentgateway-upstream#35, − the named buffer error on an MCP upstream's response, reverted by giantswarm/agentgateway-upstream#36; 2.2.1 is tag `v2.2.1` on `13325b82` = 2.2.0 + a resume refusal the control plane marks not resumable answered at once with its own status, giantswarm/agentgateway-upstream#29, which the golden refusal of #80 needs; 2.2.0 is tag `v2.2.0` on `71f1adf6`, the agentgateway line's 2026-09-24 re-pin onto upstream `main` @ `3528a428` plus its carried patches and the grpc pin; before it `2.1.2`, tag `v2.1.2` on `39023841`), which descends from the dev build the 2026-09-22 re-pin chose (`0.0.0-dev.giantswarm.2026-09-22.11-31-30.h6afcce9` = upstream agentgateway `main` @ `08f4563b`, 2026-09-21, ≥ `8dba3989`: #3411's egress credential injection and #3524's provider contract, which beta4's and beta5's charts pin as `ghcr.io/agentgateway/agentgateway:v0.0.0-alpha.8dba3989`) plus the line's carried patches; the agentgateway line moved to `main` @ `3528a428` on 2026-09-24 without a release yet, so this line ran 2.1.2 until that release, `2.2.0`, existed (a released build, as the platform's image policy requires). Since 2.1.1 a file resource is read from disk on every managed fetch, so an egress CA switch reloads `tls.crt` and `tls.key` as one pair: the dev build and upstream's alpha reload them as two events and serve leaves signed with the other file's key in between, which fails every handshake of that window. Since 2.1.2 the files a parse reads are watched as well. `release-1.0` pins the same release. |
| When it moves | only together with kagent's pin, proven in agentlab first (`agentlab configure --defaults --chart-branch poc/kagent-main && agentlab up` and the proofs) — see "Re-pin". Not on a schedule. |
| Derived how | the nearest upstream `v*` tag reachable from the line, prereleases included (`git tag --merged` against `git ls-remote --tags upstream`, nearest by commit distance — a version sort would place a prerelease behind its release): the line's own tags version the package (see "Versions") and share the `v*` namespace, so they never count — and upstream `main`, rebased onto agent-substrate, has no release tag as an ancestor, so a merge-base with it cannot serve. The sync workflow computes it on every run, weekly ones included, and fails with the reason when no upstream tag is reachable; until 2026-10-08 it counted stable tags only, so every weekly run from 2026-09-28 on failed at this step while the line pinned v0.2.0-beta5, and the mirror stayed where the last run had left it (#274, #276). `.circleci/config.yml` states it once, as the annotation `io.giantswarm.upstream.version` every image carries. |

## Carried patches

Everything on `giantswarm` that is not in the pin (`git log v0.4.0-alpha1..giantswarm`) has a ledger file of its
own in [`fork/`](fork/): one small file per change, so two concurrent pull requests never edit the same lines.
The directory is the index (`ls fork/`); this file carries no per-change rows. A file reads:

```markdown
# <short title>

- **Status:** carried
- **Upstream:** the upstream pull request or issue, with its state
- **Our commit:** the commit on `giantswarm`, replayed at every re-pin

## Change

What the commit does.

## Reason

Why the line carries it.
```

A change adds or edits only its own file, in its own pull request; the file's name is the change's key. A re-pin
that makes a change redundant deletes its file (the paragraphs below name what each re-pin dropped). The
fork-infrastructure file (CI, publishing, sync, the ledger check) covers the changes to files that are not
Substrate's own.

Thirty-two patches change Substrate ahead of upstream — egress for an actor while it resumes, without which no
skill-carrying agent of the platform boots, the atelet scheduling knobs and its rolling update, without which a release steps a 20-node cluster past its HelmRelease timeout, the keep policy on the CRD chart's
templates, the kept podcertificate-controller namespace, without which a reinstall on the same cluster has
no working trust chain, the terminal classification of an image the registry refuses, without which a
misconfigured Harness image is never reported, the re-registration of a worker whose pod IP changed, the
pinned image set with its chart keys and the file atelet re-reads them from, and the lifecycle budgets (a workflow that outlives its caller, the
router's parking budget, the bounded restore), without which the first turn after an idle spell hangs for
good, the re-asserted capacity report, without which a worker re-registered under a running ateom is never placed
on again, and the ServiceAccount annotations with ate-api-server's `extraEnv`, without which a pod identity for an
external snapshot store is patched in after the install, and the pool template's spread and anti-affinity, without which
one node loss takes a whole worker pool, and the fast, terminal answer for a paused actor whose snapshot's node is gone, without which
such a session waits out every parking budget and can neither be resumed nor deleted, and the durable pause, without which
a paused session lives and dies with the one node it paused on, and the bounded golden boot, without which
a workload that exits at start-up is re-booted forever and its cause never reported, and the cross-certified CA rotation, without which
the egress MITM CA cannot be rotated without failing actor TLS, and the image-layer owners, without which
an actor run as its image's `USER` cannot write what its image gave it, and the migration of the golden snapshot status a release before golden tags recorded, without which
every template created on 1.0.x refuses its actors as not resumable after the upgrade, and the egress-policy contract check, without which a `kubectl-ate` of another line stores a policy with another meaning, and the template delete that takes the row first, without which a refused delete leaves a template whose golden is gone, and the fencing token on Pause, Suspend and Resume, without which a lease holder frozen past its lease pauses an actor its successor left running, and the lease-free durable-pause commit with the holder-naming lease, without which a client's pause or suspend right after a resume is Aborted by background work on a loaded database, and the denial's own gRPC code for a denied gRPC call, without which a gRPC caller of a held actor reads Unavailable and cannot tell it from an unavailable control plane, and the re-take of a golden written in an older snapshot layout, without which every new conversation on every agent fails after a release that changes the layout until the goldens are reset by hand, and the retry of a golden take a rolling worker pool interrupts, without which an upgrade that rolls the pool under the re-take fails every golden for good, and the retry of a resume a rolling worker pool interrupts, without which a session resumed during the roll crashes for good, and the crash that records its worker leaving, without which a restore a worker drain cuts crashes for good; all twenty-eight are written for upstream and leave at the first release that carries them. The image-identity patches are upstream's open
agent-substrate/substrate#1918 and #1906 with the merged #1910. The Postgres Secret patch is upstream's own pull
request, a draft since 2026-09-29. Everything else is what upstream has already merged; the `x/net` v0.61.0 requirement changes versions only and leaves when upstream requires it (see its row). Giant Swarm specific wiring
lives elsewhere: the CA/JWT pool bootstrap (`kubectl-ate admin make-ca-pool`/`make-jwt-pool`
and the `ate-api-authentication` ConfigMap) is created by [agentlab](https://github.com/giantswarm/agentlab)
and by meta chart 4.0; the `WorkerPool` the platform's Harnesses run on comes with the kagent chart
(`kagent.substrateWorkerPool`); feature gates, Kyverno exceptions and network policies are cluster
configuration.

Dropped at the re-pin onto v0.0.29 (2026-09-14): the `cherry-pick -x` of kagent-dev/substrate#33 (atelet's
cluster-wide read of sandbox configs, `74b45f9e` → upstream `af7d1539`, in v0.0.29 — [#37742](https://github.com/giantswarm/giantswarm/issues/37742)
row 10 closes) and the chart half of #9 (the egress actor check as a frontend policy, which kagent-dev/substrate#28
made under agentgateway#3318's name `substrateEgressActorResolution`; the line's dataplane `v1.5.1-gs.4` carries
#3318). The rebase conflicted where expected: the agentgateway pin (upstream's alpha image vs the line's release —
the line's), `cmd/ateom-gvisor/main.go` (the terminate patch vs #1496/#1549 — hand-merged), and README/values
hunks of the chart patches beside upstream's OTLP keys (both kept); upstream's new `workflow_delete_test.go`
(#1541) constructs the workflow without the bounded restore's budget and was given it inside that patch.

Dropped at the bootstrap of the line (2026-09-10): the three June commits of the old fork `main` — `6564754e`
enable websockets (upstream has it: `cmd/atenet/internal/router/xds.go` `UpgradeConfigs`), `5169cdd4`
ActorTemplate env refs (upstream kagent-dev/substrate#20, merged), `63ea2b0e` a dispatch-only release workflow
that never ran (upstream ships `release.yaml`) — and the eight stale June branches whose patches upstream has
merged.

Dropped at the 2026-09-22 re-pin onto v0.2.0-beta4 because upstream carries the fix: the runsc-terminate patch
(`e1e2ebe0`, [#30](https://github.com/giantswarm/substrate/pull/30); upstream kagent-dev/substrate#1666, `2dda7260`, treats a
container runsc has no record of as already destroyed, and upstream's sandbox-teardown test refuses a runsc without
`list`). Adapted in the replay, same behaviour: the egress-while-resuming patch lost its authenticated-identity log
(upstream replaced it with the policy evaluation), the pod-IP re-registration reads the WorkerPool from the informer
indexer and no longer writes the immutable sandbox class, the restore budget and the node-gone patches pass through
upstream's new `NewRPCService` (identity issuer and pools) and the node lister, the durable pause names its upload
destination through a helper on upstream's `InProgressSnapshotUri` (upstream stores the URI, not the name) and commits
the durable copy as it is only when the copy carries the scope the template commits — a Full pause copy under a Data
commit is uploaded anew from the node, as upstream's lifecycle e2e (`onCommit:Data, onPause:Full, suspend from PAUSED`)
requires; the platform's templates leave both scopes at the Full default, so nothing changes for them — the
golden-boot bound sits beside upstream's golden tags; protobuf, python stubs and validation code regenerated with
`hack/update/codegen.sh`. The fork commits above are the replayed SHAs.

Nothing dropped at the 2026-09-24 re-pin onto v0.2.0-beta5: upstream merged none of the carried patches between beta4 and
beta5 (kagent-dev/substrate#46, the PodMonitors, is still open). Adapted in the replay, same behaviour: the chart patches'
README rows sit beside upstream's new `otel.endpoint` text (#45's lifecycle events), the node-gone functional test sits
beside upstream's `RevertActor` tests (#1675) and the fake atelet carries upstream's `Terminate*` fields next to the durable
pause's `Prune*`, the make-ca-pool half of the CA rotation moved into `cmd/kubectl-ate/internal/cmd/admin.go`, where
upstream #1733 consolidated the admin subcommands; protobuf and python stubs regenerated with `hack/update/codegen.sh`.
The fork commits above are the replayed SHAs.

Dropped at the 2026-10-05 re-pin onto v0.3.0-alpha3: the otlploggrpc bump (`e3671af8`, [#93](https://github.com/giantswarm/substrate/pull/93)), because alpha3 pins
`otlploggrpc`, `otel/log` and `otel/sdk/log` v0.21.0 and converts `slog` values through `attribute.Value` itself (clamping an unsigned
value above `math.MaxInt64`). Replaced: the line's own default egress policy (`c87cc0d5`, #56) by the `cherry-pick -x` of
agent-substrate/substrate#1558 (`c7de422d`, `7ae519bd`, `6c7e6b97` → `0863afa8`, `13d36629`, `323e69bd`; the review fixup of the NotFound test
message folded), its tests re-seated on upstream's GA egress rules (`http`/`https`/`tls_passthrough`, #1751) with the port the server defaults (the
suites on the sync branch caught the bare rules), and the template's rules under the policy's tie check
(`ValidateCustom_EgressPolicyTemplate_Rules`, a `+k8s:customValidation` marker on `EgressPolicyTemplate.rules`), the decision of 2026-09-29.
Reworked on alpha1's crash model (every failed atelet Run/Restore crashes the actor, agent-substrate/substrate#1220; the `ateerrors`
taxonomy deleted, #1817), carried over from the 2026-09-29 probe: the image-refusal row keeps the template failure with the recorded
crash (`0398c88e`), the restore budget returns a typed `restoreTimedOutError` (`18ecc6ac`), the node-gone row (`f3d05a0c`) and the golden-boot bound
(`2d504183`) pass through upstream's constructor and crash path. Adapted in the replay, same behaviour: the egress-while-resuming patch takes
the restore checkpoint directory from upstream's `ActorDirs.RestoreDir`; the micro-VM restore forwards logs over the agent connection
upstream now dials before the restore; the CA rotation's probe fixture sits beside upstream's `header=` parameters, redirect policy and
body echo, its suite beside the `E2E_ATENET_DATAPLANE` switch, and its two helm-e2e lanes carry `E2E_JUNIT_FILE` for upstream's lane
manifest; the durable copy lives on `LocalSnapshot` (upstream renamed `LocalSnapshotInfo`); the CAP_DAC_OVERRIDE manifest comment
takes upstream's `/var/lib/ate` wording (#1910 is in v0.3.0, not in alpha3, so the patch stays); the USER and WORKDIR patches build on
`cmd/atelet/internal/ateletpath` (upstream moved the actor paths out of `ateompath`) and beside `ocispec.Options`' directory fields and
`MicroVMOptions.ActorDirs`; the gVisor worker image takes its `image` parameter beside the `systemNamespace` and ServiceAccount
parameters upstream added to `buildDeploymentApplyConfig`, with the chart test counting their two arguments. Protobuf and validation
code regenerated with `hack/update/codegen.sh`. The fork commits above are the replayed SHAs.

## Upstream items prepared without a carried patch

Items the platform needs from Substrate whose fix is not yet written. The text below is the upstream-facing
proposal (upstream has issues disabled, so it becomes a kagent-dev/substrate pull request when the patch
exists, or a kagent-dev/kagent issue); it is listed in [#37742](https://github.com/giantswarm/giantswarm/issues/37742)
and opened only after the team has reviewed that list.

### Place an actor's restore only on a worker whose CPU covers the snapshot's feature set ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 12)

**Problem.** A snapshot — the golden snapshot of an `ActorTemplate` version and an actor's own — is a gVisor
checkpoint, and `runsc restore` loads it only on a host whose CPU offers every feature the checkpoint recorded
(gVisor `pkg/cpuid`, `FeatureSet.CheckHostCompatible`: `incompatible FeatureSet: missing features: …`). The
scheduler knows nothing about the CPU: `scheduling.Constraints` carries the sandbox class, the template and
actor label selectors, `RequiredNodes` (a PAUSED actor's local snapshots) and the resource limits
(`cmd/ateapi/internal/scheduling/scheduling.go`, `schedulingConstraints` in
`cmd/ateapi/internal/controlapi/workflow_resume.go`), and a `Worker` reports its node name, its pool's labels
and its capacity, never its CPU (`pkg/proto/ateapipb/ateapi.proto`, `Worker` / `WorkerStatus`). A
`WorkerPool` whose nodes an autoscaler provisions can span vendors under one architecture — Karpenter
consolidation put AMD `c5a` and Intel `r6i` nodes under one amd64 pool on a Kubernetes 1.35 cluster on
2026-09-13 — and then a restore is placed on a worker that cannot load the checkpoint: `RestoreWorkload` fails
with the line above, the actor stays `RESUMING` on its claimed worker (`validateAssignedWorker` finds it live,
owned and `Applies`), the retry runs on the same worker every 30 s with the same result, and the actor never
recovers; the consumer sees every turn time out (kagent: `actor "ai-…" request timed out`). The golden snapshot
had been taken on an AMD worker; the missing features were `cmp_legacy`, `cr8_legacy`, `sse4a`, `misalignsse`,
`topoext`, `mmxext`, `fxsr_opt` — AMD-only CPUID flags. The consumer's workaround is a `nodeSelector` on the
pool naming one vendor (`karpenter.k8s.aws/instance-cpu-manufacturer`), which is a promise the pool owner has to
keep by hand and which does not cover a generation change within one vendor.

**Proposal.** Make the feature set a placement constraint, the way local snapshots already are:

1. **Workers report their CPU feature set.** ateom reads the host's feature set at start — the same
   `pkg/cpuid` gVisor checkpoints it with (`HostFeatureSet()`), so the comparison is exact — and reports it
   with its capacity (`internal/ateomcapacity`), into a new output-only `WorkerStatus.cpu_features`
   (the canonical sorted feature list, or gVisor's string form). The Syncer never sets it; the ateom does,
   as it does capacity.
2. **Snapshots record the feature set they were taken with.** `CheckpointWorkload` returns the checkpointing
   worker's set; ate-api stores it on the `ExternalSnapshot` (and on `LocalSnapshotInfo`) it writes, and the
   golden snapshot inherits it into `GoldenSnapshotStatus.golden_snapshot`.
3. **The scheduler requires a covering worker.** `scheduling.Constraints.RequiredCPUFeatures`, derived in
   `schedulingConstraints` from the snapshot the resume will load — the actor's external snapshot, else the
   template's golden snapshot; empty for a cold boot — and enforced in `Applies`: the worker's reported set
   must be a superset. A `RESUMING` actor whose assigned worker does not cover the set is released and
   rescheduled (the not-eligible branch of `validateAssignedWorker` already releases the claim) instead of
   retried in place. `ErrNoCapacity` then names the missing features when no worker covers them, so a pool
   whose nodes have all moved to another vendor fails loudly rather than silently.
4. **Fail fast meanwhile.** Independently of 1–3, classify the runsc `incompatible FeatureSet` restore error as
   a placement failure: release the worker, exclude that worker's node for this actor
   (a new `ateattr` reason next to `ReasonWorkerReassigned`), and reschedule — the same shape as the terminal
   image-pull classification this fork carries. This alone unwedges the actor on the next resume when the
   pool still has a covering worker, and it is the small first patch.

A golden snapshot per feature set (one golden actor per distinct set present in the pool) would remove the
constraint altogether but multiplies the golden boots; the placement constraint is enough while pools are
homogeneous most of the time. gVisor offers no checkpoint-portability mode (features cannot be masked at
checkpoint), so the fix is placement, not the checkpoint.

**Alignment.** [kagent-dev/substrate#19](https://github.com/kagent-dev/substrate/pull/19) (enforce the required
`WorkerPool` placement, open since July) extends the same `Constraints`; it pins the pool, this pins the CPU
within a pool — the two compose and should share the eligibility plumbing. Nothing upstream tracks CPU
features today (searched 2026-09-13: issues are disabled, no pull request names `FeatureSet`, `cpuid` or the
restore error).

## Re-pin

The re-pin moves the line onto a new upstream release tag and replays the carried patches; a patch upstream has
merged falls away by itself (`git rebase` drops already-applied patches). It is the one sanctioned rewrite of
`giantswarm`.

1. The pin is whatever upstream kagent `main`'s `go/go.mod` `replace` names — the ate-api contract is versioned by
   it, and since kagent-dev/kagent#2802 the router protocol too (`ate-target-actor`). The three lines move in one
   order: the agentgateway line first (this chart pins its release for the router — a `main` commit ≥ the one
   upstream's chart pins), then this line, then `giantswarm/kagent-upstream` onto the upstream head whose `go.mod`
   names the new pin. Never move Substrate ahead of the agentgateway release it needs, and never kagent ahead of
   Substrate: a kagent past #2802 on a 0.0.27 Substrate is green in CI and fails every turn on a cluster.
2. Run **Actions → sync-upstream → Run workflow** with `pin` = the tag (for example `v0.0.27`); `line` names a
   maintenance line (`release-1.3`) instead of `giantswarm`. The workflow mirrors `main`, rebases the carried
   patches onto the tag, runs `go build ./... && go test ./...`, pushes the candidate to `sync/<date>-<tag>` and
   opens a pull request against the line that lists the replayed patches and records the line head it was
   replayed from; it closes the pull requests of the candidates it supersedes and deletes their branches, every
   `sync/*` branch but one an open pull request against another line holds. The candidate's push runs the line's suites on its head (`pr-workflow` with the required
   `run-tests`, `helm-e2e`, `govulncheck`) — the pull request itself runs nothing, a rebased branch has no merge
   commit.
   - On a conflict the candidate holds the patches that applied before it and the pull request names the
     conflicting patch and the ones behind it. Finish it by hand on the same branch: check it out,
     `git cherry-pick -x` the rest, resolve, test, push it — which runs the suites on the new head.
   - Review the candidate in its pull request. Once `run-tests` and `govulncheck` passed on its head, run the
     workflow with `land` = the candidate branch (and the same `line`): it refuses a candidate whose required
     checks did not pass on its head or whose recorded line head is no longer the line's, pushes exactly that
     head to the line, closes the pull request and deletes the branch (GitHub may already show the pull request
     merged and the branch deleted once the line holds its head; the land comments on it instead). **Never merge it**, through GitHub or `devctl pr merge`: the
     line is a rebased branch, no merge method lands a rewrite (a rebase merge replays upstream's commits onto the
     old line and conflicts), and a merge would fold the old pin back in. The push to `giantswarm` publishes the
     dev build.
   - `dry_run: true` does everything except the pushes; the run summary shows the candidate, the pull request it
     would open, the candidates it would prune and, for `land`, the checks and the push it would make.
3. In a pull request: this file (the pin), the ledger files of `fork/` (the carried patches), the pin annotation of the six push jobs in
   `.circleci/config.yml` (`index:io.giantswarm.upstream.version=<tag>` — every image carries the pin from there),
   and the Substrate rows of #37742.
4. Move the consumers to the new dev version (see "Consumers"), prove it in agentlab, then let the meta chart's
   pin and kagent-upstream follow.

The 2026-09-22 re-pin (v0.0.29 → v0.2.0-beta4) by hand, in a scratch clone: `git rebase --onto v0.2.0-beta4 v0.0.29`
replayed 63 of the 64 carried commits (the runsc-terminate patch dropped, above), with hand resolutions in the egress
gateway, the worker syncer, ateapi's `main.go` and functional tests, `workflow_suspend.go` and the template reconciler
(upstream's constructor, listers, URI model and golden tags moved under the patches), and a regeneration of the generated
code; the chart image patches conflicted only on the `images.agentgateway` line, resolved to the patch's own value each
time. `go build ./... && go vet && go test ./cmd/... ./internal/...` and `helm unittest` locally, the suites in CI on the
`sync/20260922-v0.2.0-beta4` push; the force-push by a repository admin. The dev builds of the re-pinned branch start
from `0.0.0` again until the next release tag (the line's stable tags point at the pre-rebase history, which gitsemver
no longer reaches), so a consumer pins the full dev version.

The 2026-09-24 re-pin (v0.2.0-beta4 → v0.2.0-beta5) by hand, in a scratch worktree: `git rebase --onto v0.2.0-beta5
v0.2.0-beta4` replayed all 84 carried commits (nothing dropped, above), with hand resolutions in the chart README rows,
the functional tests' placement and fake atelet, the generated protobuf code and the kubectl-ate admin file upstream
consolidated. `go build ./... && go vet ./... && go test ./cmd/... ./internal/...`, `helm unittest charts/substrate` and
`hack/verify-all.sh` locally (`internal/childreap`'s p90 timing test failed once under a loaded workstation and passed
again on the same tree); the suites in CI on the `sync/20260924-v0.2.0-beta5` push; the force-push by a repository admin — who first lifts the admin bypass of the
repository manager's `devctl: default branch` ruleset (present since 2026-09-24; it admits admins for pull requests only
and refuses a force-push otherwise) to `always` and restores it right after.
The dev builds start from `0.0.0` again, so the consumers pin the full dev version.

The 2026-10-05 re-pin (v0.2.0-beta5 → v0.3.0-alpha3) by hand, in a scratch worktree, from the 2026-09-29 probe candidate
(`sync/20260929-v0.3.0-alpha1`, the carried stack already replayed onto v0.3.0-alpha1): `git rebase --onto v0.3.0-alpha3 v0.3.0-alpha1`
of its 103 commits, then the 34 commits `giantswarm` gained since 2026-09-29 cherry-picked in order; the hand resolutions and the one
drop are listed under "Carried patches". The 54 `docs(fork)` ledger commits of the beta5 line (every FORK.md edit since the bootstrap,
23 of them release-table rows) are folded into this ledger commit: the replayed stack is 83 commits, 60 code patches and 23 of the
fork's own CI, publish and alignment changes. The pin annotation moved to v0.3.0-alpha3 (`0cfdba6c`) and the data plane to the agentgateway
line's 2.3.0 (`a07cc154`) in the same branch, as the chart's egress config and the `ateom-for-actor` identity of alpha3 require. `go build ./... &&
go vet ./... && go test ./cmd/... ./internal/...`, `helm unittest` on both charts and `hack/verify-all.sh` locally (`hack/verify/metrics.sh`
needs `--security-opt label=disable` on an SELinux-enforcing workstation, `DOCKER=` a wrapper); the suites in CI on the
`sync/20261005-v0.3.0-alpha3` push. Before the force-push the last candidate of the beta5 pin, `v1.4.0-rc.7` (`81abb52e`), is promoted
to `1.4.0` (`Auto-release`, `release-type: stable`): the push to `giantswarm` cuts the next candidate from the line's highest stable tag
(the workflow moves that baseline onto the commit the re-pinned branch still shares with it), so a re-pin pushed on top of `1.4.0-rc.7`
would have been numbered `1.4.0-rc.8` and the `1.4.0` line of agent-substrate/substrate#1918 (the image `USER` stack kagent 1.3.0 needs)
would have no stable release. The force-push by a repository admin with the `devctl: default branch` ruleset's admin bypass lifted
to `always` and restored to `pull_request` right after.

The 2026-10-08 re-pin (v0.3.0-alpha3 → v0.4.0-alpha1) by hand, in a scratch worktree: `git rebase --onto v0.4.0-alpha1 v0.3.0-alpha3`
of the 130 commits of `giantswarm` at `47c6868e`. One applied upstream on its own (`git cherry`); three are dropped because upstream carries
them: the `OTEL_METRICS_EXPORTER=none` patch (#82, agent-substrate/substrate#1878 `1e991977`), the redaction of the PostgreSQL connection
string at startup (agent-substrate/substrate#1833 `38b9522f`), and the `GOLDEN_SNAPSHOT_UNAVAILABLE` refusal (#80), whose only site, the
`onResume.fromData` golden resolution, upstream removed (agent-substrate/substrate#2105); the agentgateway and kagent readers of its
ErrorInfo stay in their lines and no longer see it. Hand resolutions: the egress-during-resume split onto `internal/ateomtunnel`, the
PostgreSQL Secret onto the read/write and owner DSNs, the worker re-registration onto repeated `ips` and `epoch`, the restore budget onto
`handleAteletError`, the durable pause onto the power-of-two-choices scheduler (`candidatesOnNodes`) and upstream's release of a replaced
external snapshot on suspend, the lease holder onto upstream's batched lease cleanup and its worker-assignment reconciler, the golden
status migration (#151) without its resume hunks, the `OTEL_TRACES_EXPORTER=none` patch on `tracesPushEnabled`, the template egress
rules validation and the egress-policy contract RPC beside upstream's `apivalidation` package, the chart tests without the dropped
`--gcp-auth-for-image-pulls` argument, and generated code (`hack/update/codegen.sh`) after every patch that changes a proto. The line's own
migration moves to a ledger of its own (`migrations_fork/`, `fork_schema_migrations`; see its row), so upstream's `000003_access_policies.sql`
stays version 3 and a 1.5.x database is moved once at startup. The template's default egress policy, stored with the actor since
upstream's store takes the metadata from its caller (`setCreateMetadata`), is built with `{atespace, name: "default"}`, as the RPC
requires of a policy. `go build ./... && go vet ./... && go test ./...`, `helm unittest charts/substrate` (78/78) and the
`hack/verify/` scripts (`proto-fmt` with clang-format 18.1.8) locally; the suites in CI on the `sync/20261008-v0.4.0-alpha1` push. The
pin annotation moves to v0.4.0-alpha1 in `.circleci/config.yml`, the egress-policy contract to `v0.4.0-alpha1` (`CredentialHeader.actor_jwt`). The data plane stays on the agentgateway line's 2.3.0: v0.4.0-alpha1 needs only agentgateway#3689, which 2.3.0 already carries.

The weekly run (Mondays 05:23 UTC) does not re-pin: it mirrors `main` and **probes** whether the carried patches
still rebase onto upstream `main`, naming the first patch that would conflict in the run summary, so the next
re-pin is never a surprise.

**Identity of the automation.** The workflow pushes as the org's **HeraldBot GitHub App** — a token minted per run
from the org secrets `HERALD_CLIENT_ID` / `HERALD_APP_KEY` (`actions/create-github-app-token`); the App is installed
on every org repository with contents and workflows write access. Why an App and not the org's machine-account token: GitHub refuses a push from a personal access token
that creates or changes a file under `.github/workflows/` unless the token carries the `workflow` scope, and upstream
`main` — hence every mirror and every re-pin — carries upstream's workflow files; the first run (2026-09-10, with
`TAYLORBOT_GITHUB_ACTION`) failed exactly there. A push with the workflow's own `GITHUB_TOKEN` would not do either: it
triggers no other workflow, while the candidate's push is what runs the required checks on it and the push to
`giantswarm` is what publishes the dev build. The App (`Integration` 414149) alone may replace `main` and, for a
landing, the line; on the line it gets no way around the required checks — a push whose head has not passed
`run-tests` and `govulncheck` is refused for every actor, so a re-pin lands only as the candidate the checks ran on.
Manual fallback for `main` is a `workflow_dispatch` of the sync; a re-pin of the line built by hand is pushed as a
candidate and landed by the workflow, never pushed to the line (the `main` mirror was bootstrapped by hand on 2026-09-10: `git push --force-with-lease=refs/heads/main:<old> origin
upstream/main:refs/heads/main` — which also started upstream's suites on `main`, hence the cancel step).

A candidate built by hand (a workstation, upstream as a remote) lands the same way:

```sh
git fetch upstream main --tags
git checkout giantswarm
git rebase --onto v0.0.30 v0.0.29          # new pin, old pin
go build ./... && go test ./... && helm unittest charts/substrate
git push origin HEAD:sync/$(date -u +%Y%m%d)-v0.0.30   # the suites on the candidate
# a pull request against giantswarm whose body ends with "Line head: <the giantswarm head it was rebased from>",
# then, once run-tests and govulncheck passed on the candidate's head:
gh workflow run sync-upstream.yaml --repo giantswarm/substrate -f land=sync/<date>-v0.0.30
```

## Publishing

The CircleCI pipeline `.circleci/config.yml` publishes to `gsoci.azurecr.io/giantswarm/substrate` on every push
to `giantswarm` (a dev build) and on every release tag `vX.Y.Z` or `vX.Y.Z-rc.N` — six `architect/push-to-registries`
jobs of the architect orb, one per component (built from `.circleci/Dockerfile` for linux/amd64 and linux/arm64;
the index signed, with provenance and an SPDX SBOM, annotated `io.giantswarm.upstream.version=<pin>`) → `scan`
(Trivy, per image) → `charts` (stamped, packaged, pushed, signed). The version is the orb's, from the git tags
(see "Versions"); the pipeline computes none. Every other branch runs the same six builds with `push: false`: a
pull request validates the Dockerfile for both platforms and nothing leaves the builder. Nothing is ever pushed by
hand, nothing publishes from GitHub Actions, and nothing pushes to ghcr.io: the registry credentials exist in
CircleCI's `architect` context only, and the signing identity is the pipeline's own.

| Artifact | Name |
|---|---|
| Control plane and node images | `gsoci.azurecr.io/giantswarm/substrate/{ateapi,atecontroller,atelet,atenet,podcertcontroller,kubernetes-secrets}:<version>` (the last is `cmd/credential-provider/kubernetes-secrets`, the chart's `k8s-credential-provider` the egress gateway injects Secret-backed credentials from; `COMPONENT_PATH` names its source directory) — linux/amd64 + linux/arm64, `.circleci/Dockerfile` with `COMPONENT=<name>`: `./cmd/<name>` compiled as ko compiles it (`CGO_ENABLED=0`, `-trimpath`, the vendored modules, the VCS revision embedded) at `/ko-app/<name>` on the distroless base `.ko.yaml` pins, `/ko-app` on `PATH`, the binary the ENTRYPOINT; the index signed, with provenance and SBOM |
| Worker image | `gsoci.azurecr.io/giantswarm/substrate/ateom-gvisor:<version>` — the `WorkerPool.spec.workerImage` of the platform's pool, built the same way; the index signed |
| agentgateway | not built or mirrored here: `images.agentgateway` is stamped to a **release of the Giant Swarm line of agentgateway**, published by that line from CircleCI under its own gsoci path — `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:<X.Y.Z>` (how it gets there is that line's `FORK.md`), the `agentgateway-image` parameter of `.circleci/config.yml` — which atenet-router and atenet-egress run ([giantswarm/agentgateway-upstream `FORK.md`](https://github.com/giantswarm/agentgateway-upstream/blob/giantswarm/FORK.md), tracking [giantswarm/giantswarm#37758](https://github.com/giantswarm/giantswarm/issues/37758)) |
| Charts | `oci://gsoci.azurecr.io/giantswarm/substrate/helm/substrate-crds:<version>`, `oci://gsoci.azurecr.io/giantswarm/substrate/helm/substrate:<version>` — `image.registry`, `image.repository` and `image.tag` stamped to this registry, `images.agentgateway` to the agentgateway line's release; `version` = `appVersion` = the image tag; each chart signed |

Not published from here: `ateom-microvm` and the demo images (the platform runs gVisor workers only),
`kubectl-ate` binaries (use upstream's release), PyPI packages. Third-party images are upstream's pins — the same
tags and digests — under their `gsoci.azurecr.io/giantswarm/…` copies (`postgres`, `rustfs`, `busybox`, the
agentgateway line's release, `aws-cli`; the carried patches above).

**Versions.** The line's versions are its own, decoupled from upstream's, as the company's semver rules for
packaged upstream software require (RFC "Semantic Versioning of Upstream Software"): the upstream release a
version is built from is documented here ("Pin") and carried by every image as the index annotation
`io.giantswarm.upstream.version`, never encoded in the version.

- Release: a git tag `vX.Y.Z` (`vX.Y.Z-rc.N` for a candidate) publishes the images and both charts as `X.Y.Z`
  (the tag without `v`; `appVersion` the same). **`1.0.0`** is the first release of the scheme — the next major
  above the `0.0.30-gs.N` releases, which were coupled to upstream's next patch and are superseded. Consumers
  follow a Flux `OCIRepository` range, `semver: ">=1.0.0 <2.0.0"`.
- Dev build, on every push to `giantswarm`: gitsemver 3's dev version, `<next patch>-r588f3d76t<YYYYMMDDHHMMSS>h<sha7>`
  (architect-orb 10.10.0, since 2026-09-24; `r588f3d76` is the CRC32 of the branch name, `gitsemver branch-hash giantswarm`;
  builds before it carry the superseded `<next patch>-dev.giantswarm.<YYYY-MM-DD>.<HH-MM-SS>.h<sha7>`, which the registry
  keeps). The base is the newest stable tag reachable from the commit plus one patch — `0.0.0` while none is reachable (the
  builds of a re-pinned branch, whose stable tags point at the pre-rebase history, start there again until the next release
  tag) — then the committer time in UTC, so a rebuild of a commit yields the same version and dev builds sort
  chronologically within the branch and below the release they anticipate; at one base a current tag sorts above a
  superseded one. The dev channel is a Flux `OCIRepository` with `semver: ">=0.0.0-0 <2.0.0-0"` (the floor below the base a
  re-pin restarts at) and `semverFilter: "^.*-r588f3d76t[0-9]{14}h[0-9a-f]{7}$"`, which no superseded build matches; exact
  pins name the full string.
- Image tags carry no `v`, as every Giant Swarm image tag does.

**Signatures.** Every index and chart is signed keyless with cosign under the pipeline's CircleCI OIDC identity
(issuer `https://oidc.circleci.com`, subject `https://circleci.com/api/v2/projects/<project>/pipeline-definitions/<definition>`)
— the images and their SBOM attestations by the orb's push job, the charts through the orb's `cosign-sign-verify` —
the identity the platform's image policy trusts. To check one:
`cosign verify --certificate-oidc-issuer https://oidc.circleci.com --certificate-identity-regexp '^https://circleci\.com/api/v2/projects/[a-f0-9-]+/pipeline-definitions/[a-f0-9-]+$' <repository>@<digest>`.

**Digests.** Every push job prints the digest of the index it pushed; the pipeline prints `Images <version>` and
`Charts <version>` with the digest of each artifact and stores them as the artifacts `image-refs.txt`
(`<name> <repository>@<digest>`, the agentgateway image included) of the `scan` job and `chart-refs.txt` of the
`charts` job; the platform pins by tag and verifies by digest and signature from there. Release digests are recorded
here:

| Release | Pin | Images and charts |
|---|---|---|
| **v0.0.27-gs.1** (2026-09-11, tag on `213d76b6` = v0.0.26 + #33 + the egress-while-resuming patch (#4) + the frontend-policy egress config and the agentgateway line's `v1.5.1-gs.2` dataplane (#7, #9)) | v0.0.26 | images `ateapi` `sha256:70545853…`, `atecontroller` `sha256:5f1ba422…`, `atelet` `sha256:54a7285c…`, `atenet` `sha256:db1adb6d…`, `podcertcontroller` `sha256:eca30364…`, `ateom-gvisor` `sha256:b6a59a48…` (linux/amd64 + arm64); charts `substrate` `sha256:0742fdca…`, `substrate-crds` `sha256:9d3fc4be…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2` |
| **v0.0.27-gs.2** (2026-09-11, tag on `ef304330` = gs.1 + #8: ate-api-server's PostgreSQL connection string from a Secret (`c1e4e32d`, `f06f5ef9` — the cherry-picks of kagent-dev/substrate#32) and the atelet scheduling knobs (`084d916d`), with their ledger rows) | v0.0.26 | images `ateapi` `sha256:201ef762…`, `atecontroller` `sha256:0e0c7f7c…`, `atelet` `sha256:00815e41…`, `atenet` `sha256:e5a9c2c0…`, `podcertcontroller` `sha256:828b1d14…`, `ateom-gvisor` `sha256:cca86090…` (linux/amd64 + arm64); charts `substrate` `sha256:ea0bcbae…`, `substrate-crds` `sha256:e2188cd0…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34554981306](https://github.com/giantswarm/substrate/actions/runs/34554981306), every scan clean |
| **v0.0.27-gs.3** (2026-09-11, annotated tag on `92eec3c6` = gs.2 + #12: `helm.sh/resource-policy: keep` on the three `ate.dev` CRD templates (`d37ebf63`), with its ledger row) | v0.0.26 | images `ateapi` `sha256:3949f485…`, `atecontroller` `sha256:39307b3c…`, `atelet` `sha256:b3abed76…`, `atenet` `sha256:22a408c7…`, `podcertcontroller` `sha256:23e2c76e…`, `ateom-gvisor` `sha256:f6863171…` (linux/amd64 + arm64); charts `substrate` `sha256:3883f355…`, `substrate-crds` `sha256:534b3276…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34603612441](https://github.com/giantswarm/substrate/actions/runs/34603612441), every scan clean. Consumer: the agent-platform meta chart re-pins to it (giantswarm/agent-platform#390) |
| **v0.0.27-gs.4** (2026-09-11, tag on `bbe92143` = gs.3 + #14: an actor whose image the registry refuses is crashed with the registry's answer and its template's golden boot fails with the cause (`bbe92143`)) | v0.0.26 | images `ateapi` `sha256:023a7b2a…`, `atecontroller` `sha256:7fec05e9…`, `atelet` `sha256:5fcbd743…`, `atenet` `sha256:dc10bb95…`, `podcertcontroller` `sha256:a7b743b7…`, `ateom-gvisor` `sha256:6cb5ba54…` (linux/amd64 + arm64); charts `substrate` `sha256:89d713f1…`, `substrate-crds` `sha256:a6faad30…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34604690599](https://github.com/giantswarm/substrate/actions/runs/34604690599), every scan clean |
| **v0.0.27-gs.5** (2026-09-11, annotated tag on `e47f3c80` = gs.4 + #13: `helm.sh/resource-policy: keep` on the chart's `podcertificate-controller-system` Namespace (`e73c1dc0`), with its ledger row) | v0.0.26 | images `ateapi` `sha256:689f4529…`, `atecontroller` `sha256:85a4a5e3…`, `atelet` `sha256:06226ce2…`, `atenet` `sha256:51c2f20e…`, `podcertcontroller` `sha256:a822055d…`, `ateom-gvisor` `sha256:b989620a…` (linux/amd64 + arm64); charts `substrate` `sha256:99d75b44…`, `substrate-crds` `sha256:70f6a66f…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34607610137](https://github.com/giantswarm/substrate/actions/runs/34607610137), every scan clean |
| **v0.0.27-gs.6** (2026-09-12, annotated tag on `29825f84` = gs.5 + #20: a worker whose pod IP changed in place is re-registered (`c3d78657`) + #21: atelet's pinned image set and the chart's `atelet.imageCache.*` (`58d24855`, `ffab110f`) + #22: lifecycle workflows outlive their caller and the router parks a request 30 s, the restore bounded and retried with `RESTORE_TIMED_OUT` (`24afbd45`, `95ce0c43`), with their ledger rows) | v0.0.26 | images `ateapi` `sha256:516d6c84…`, `atecontroller` `sha256:86b56a9c…`, `atelet` `sha256:984988f4…`, `atenet` `sha256:c7dd582d…`, `podcertcontroller` `sha256:2df12254…`, `ateom-gvisor` `sha256:395c3428…` (linux/amd64 + arm64); charts `substrate` `sha256:db1f2c7f…`, `substrate-crds` `sha256:d3e7b20c…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34690425122](https://github.com/giantswarm/substrate/actions/runs/34690425122), every scan clean. The `substrate` chart carries `atelet.imageCache.*` and `atenetRouter.requestParking` (budget 30 s) from this release on; the agent-platform meta chart's range `>=0.0.27-gs.5 <0.0.28-0` admits it without a re-pin |
| **v0.0.27-gs.7** (2026-09-12, annotated tag on `5fccbf08` = gs.6 + #25: an ateom keeps re-asserting its capacity report every 10 s, so a Worker record replaced under a running ateom receives its capacity within one interval (`5fccbf08`); the ledger rows follow the tag) | v0.0.26 | images `ateapi` `sha256:aa159b9e…`, `atecontroller` `sha256:8ed3e650…`, `atelet` `sha256:433fe02c…`, `atenet` `sha256:45dd771b…`, `podcertcontroller` `sha256:3a1b684c…`, `ateom-gvisor` `sha256:54679d01…` (linux/amd64 + arm64); charts `substrate` `sha256:30ce1165…`, `substrate-crds` `sha256:f7bb5954…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34698001170](https://github.com/giantswarm/substrate/actions/runs/34698001170), every scan clean. The re-assertion runs in the worker image: a `WorkerPool` takes effect once its `workerImage` is this release's `ateom-gvisor`; the agent-platform meta chart's range `>=0.0.27-gs.6 <0.0.28-0` (4.7.18 and `main`) admits it without a re-pin |
| **v0.0.27-gs.8** (2026-09-13, annotated tag on `8fbf9e60` = gs.7 + #27: the chart takes ServiceAccount annotations for atelet and ate-api-server and `extraEnv` for ate-api-server (`atelet.serviceAccount.annotations`, `ateApiServer.serviceAccount.annotations`, `ateApiServer.extraEnv`; `8fbf9e60`); the ledger rows follow the tag) | v0.0.26 | images `ateapi` `sha256:790fb55b…`, `atecontroller` `sha256:e6e3b3f8…`, `atelet` `sha256:5c0228ec…`, `atenet` `sha256:5955a99e…`, `podcertcontroller` `sha256:6a16045d…`, `ateom-gvisor` `sha256:6d8f57f0…` (linux/amd64 + arm64); charts `substrate` `sha256:cff433f5…`, `substrate-crds` `sha256:12ff400f…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34723438405](https://github.com/giantswarm/substrate/actions/runs/34723438405), every scan clean. Defaults empty: the rendered manifests are those of gs.7; the agent-platform meta chart's snapshot-store block (giantswarm/agent-platform#411) sets the three keys, its `components.substrate` range `>=0.0.27-gs.6 <0.0.28-0` admits the release without a re-pin |
| **v0.0.27-gs.9** (2026-09-13, tag on `36d00f80` = gs.8 + #30: the gVisor worker's terminate collects a workload whose runsc containers are already gone (`1dd5d1fb` — `cleanupContainers` lists the containers runsc knows and checks and deletes only those; a delete that fails after removing its container counts as done), with its ledger row; giantswarm/giantswarm#37773) | v0.0.26 | images `ateapi` `sha256:cb267526…`, `atecontroller` `sha256:f2c845ab…`, `atelet` `sha256:0882ecb1…`, `atenet` `sha256:72939cda…`, `podcertcontroller` `sha256:d3eab8cc…`, `ateom-gvisor` `sha256:2240d996…` (linux/amd64 + arm64); charts `substrate` `sha256:bb6ba693…`, `substrate-crds` `sha256:09e363c6…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34783226348](https://github.com/giantswarm/substrate/actions/runs/34783226348), every scan clean |
| **v0.0.30-gs.1** (2026-09-14, tag on `21027aa1` = the re-pin onto kagent-dev/substrate **v0.0.29** with the ten carried patches replayed, the agentgateway line's `v1.5.1-gs.4` pinned for the router and egress gateway, the suites on `sync/**` pushes; the first release of the new base — cut after the agentlab proof of the three lines together on the same tree's dev build `0.0.30-dev.giantswarm.2026-09-14.19-48-17.h21027aa`: `platform-test` 7/7, `agents-test`, three `skills-test` fixtures (public, `frontmatter-fields`, private with `credentialRef`), `a2a-test` 5/5, a unary A2A turn outliving its caller's deadline, `toolsets-test` 11/11, as an upgrade from `0.0.27-gs.9`) | v0.0.29 | images `ateapi` `sha256:79816517…`, `atecontroller` `sha256:baaffc43…`, `atelet` `sha256:a254fdad…`, `atenet` `sha256:aa0a7f7c…`, `podcertcontroller` `sha256:02ccb3e3…`, `ateom-gvisor` `sha256:cab2bb94…` (linux/amd64 + arm64); charts `substrate` `sha256:abb39761…`, `substrate-crds` `sha256:8beaea23…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34895330411](https://github.com/giantswarm/substrate/actions/runs/34895330411), every scan clean. Consumers: kagent-upstream `Makefile` `SUBSTRATE_VERSION` (giantswarm/kagent-upstream#37 → `v0.11.0-gs.14`), the agent-platform meta chart's ranges `>=0.0.30-gs.1 <0.0.31-0` |
| **v0.0.30-gs.2** (2026-09-15, annotated tag on `41c1a66a` = gs.1 + #35: a paused actor's resume fails fast when its node is gone and crashes it when no durable snapshot remains, `LOCAL_SNAPSHOT_GONE` (`860f257d`; giantswarm/giantswarm#37795, #37742 row 44a — its ledger row follows from that pull request's session) + #33: `WorkerPool.spec.template` takes `topologySpreadConstraints` and `podAntiAffinity` (`6f2ef0d2`, with its ledger row `41c1a66a`; giantswarm/giantswarm#37797, #37742 row 46); cut after the agentlab proof of #33's dev build `0.0.30-dev.fork-workerpool-spread.2026-09-15.10-40-00.hd810518`: the spread stored unpruned on the WorkerPool and on the Deployment's pod template, owned by `workerpool-controller` under server-side apply, `platform-test` 9/9 and `agents-test` green) | v0.0.29 | images `ateapi` `sha256:8e066d15…`, `atecontroller` `sha256:3d1d4e27…`, `atelet` `sha256:6dfee809…`, `atenet` `sha256:cf5002a9…`, `podcertcontroller` `sha256:f2335b9d…`, `ateom-gvisor` `sha256:9ffc464c…` (linux/amd64 + arm64); charts `substrate` `sha256:1d2c0d7b…`, `substrate-crds` `sha256:fb336334…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34978676815](https://github.com/giantswarm/substrate/actions/runs/34978676815), every scan clean. Consumers: the agent-platform meta chart's `workerPoolSpreadFloor` and Substrate range floor (giantswarm/agent-platform#472); the kagent line stamps it at its next re-pin — the worker image is functionally gs.1's (the release changes ate-controller, the CRD and ate-api only) |
| **v0.0.30-gs.3** (2026-09-15, tag on `653de64e` = gs.2 + #34: every pause durable — the pause snapshot uploaded in the background and recorded as the actor's external snapshot marked with its source, the snapshot's node a placement preference once the copy exists, a suspend adopting the copy without the node (`653de64e`)) | v0.0.29 | images `ateapi` `sha256:4bb59829…`, `atecontroller` `sha256:9de1965d…`, `atelet` `sha256:8079258f…`, `atenet` `sha256:515de099…`, `podcertcontroller` `sha256:19e628ae…`, `ateom-gvisor` `sha256:4ad380f2…` (linux/amd64 + arm64); charts `substrate` `sha256:c08d9122…`, `substrate-crds` `sha256:71cf502e…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34981525932](https://github.com/giantswarm/substrate/actions/runs/34981525932). Consumers: the agent-platform meta chart's range `>=0.0.30-gs.1 <0.0.31-0` admits it without a re-pin — gazelle rolled it at the next 10-minute reconcile of its OCIRepository (chart pushed 14:28Z, HelmRelease Ready on gs.3 with every pod and all 16 atelets updated at 14:40Z); its one `PAUSED` actor is the incident's dead session, which the resync reports every minute as `No atelet on the node holding the pause snapshot; not uploaded` until #35 crashes it |
| **v0.0.30-gs.4** (2026-09-15, annotated tag on `d8a2d5e4` = gs.3 + #39: the golden boot a workload keeps failing is bounded — three boots counted in `GoldenSnapshotStatus.workload_boot_failures`, then the golden actor crashed (its worker freed) and the template failed with `GoldenActorNotReady`, the bound and the last boot's error — and ateom's readiness-deadline error quotes the workload's last output lines (`7a11e479`, with its ledger row `d8a2d5e4`; giantswarm/giantswarm#37801, #37742 row 47); cut after the agentlab proof of the branch's dev build `0.0.30-dev.fork-golden-boot-bound.2026-09-15.18-17-09.he684b13`: the released images loop a private-skill template without end, the patch fails it 3 min 8 s after creation with `could not read Username for 'https://github.com'` in kagent's condition message, the spec fix reaches Ready in 20 s and the failed revision is collected; `platform-test` green) | v0.0.29 | images `ateapi` `sha256:1703b848…`, `atecontroller` `sha256:4ae8e4ac…`, `atelet` `sha256:d36093c3…`, `atenet` `sha256:ddb41ec3…`, `podcertcontroller` `sha256:0fc2e4b6…`, `ateom-gvisor` `sha256:6e086b8c…` (linux/amd64 + arm64); charts `substrate` `sha256:5dfa7ae1…`, `substrate-crds` `sha256:bc5b90aa…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 35009210132](https://github.com/giantswarm/substrate/actions/runs/35009210132), every scan clean. Consumers: the agent-platform meta chart's range `>=0.0.30-gs.2 <0.0.31-0` admits it without a re-pin (the control plane's bound); the worker image — the quoted output — comes with the kagent line's stamp (giantswarm/kagent-upstream#49, `SUBSTRATE_VERSION 0.0.30-gs.1 → 0.0.30-gs.4`) through the kagent range `>=0.11.0-gs.16 <0.11.1-0` |
| **v0.0.30-gs.5** (2026-09-19, annotated tag on `ea00cfce` = gs.4 + #42/#43: every Docker Hub image default names its registry (`images.awsCli` kept a short name for the immutable bucket-init Job) + #45: the third-party image defaults are their gsoci copies, `images.agentgateway` the agentgateway line's release on gsoci + #46: the line publishes its images and charts to gsoci from CircleCI — **the first release published natively to `gsoci.azurecr.io/giantswarm/substrate`, each index and chart signed with the organisation's CircleCI identity**; cut after the agentlab proof of #46's dev build `0.0.30-dev.fork-circleci-gsoci.2026-09-19.04-30-17.hab9aa1f` from gsoci: the control plane, the atelet and the four workers on the new images, `platform-test` 13/13, `agents-test` 6/6 with the golden boot Ready in 12 s) | v0.0.29 | images `ateapi` `sha256:6692117e…`, `atecontroller` `sha256:27f95c3e…`, `atelet` `sha256:8ad7a4ec…`, `atenet` `sha256:da4e699d…`, `podcertcontroller` `sha256:6a314e07…`, `ateom-gvisor` `sha256:cf816037…` (linux/amd64 + arm64); charts `substrate` `sha256:943f8a95…`, `substrate-crds` `sha256:ebe2b126…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway:v1.5.1-gs.4` (`sha256:f3d4b52c…`); [CircleCI pipeline 6](https://app.circleci.com/pipelines/github/giantswarm/substrate/6) (`images` 1 min 57 s, `scan` 31 s, `charts` 17 s), every scan clean, every reference `cosign verify`-ed under the pipeline's identity. Consumers: the agent-platform meta chart's chart sources move to the gsoci path (giantswarm/agent-platform#580) — until then no installation follows this release, its chart range still reads the ghcr path; the kagent line's `SUBSTRATE_REPO`/`SUBSTRATE_VERSION` move together at its re-pin; agentlab's defaults (giantswarm/agentlab#229); retagger's `renamed-substrate.yaml` mirror entries are retired now that a native release exists (giantswarm/retagger#1238, open) |
| **1.0.0** (2026-09-19, annotated tag on `171b9e44` = gs.5 + #48: the six images built by the architect orb's `push-to-registries` jobs from `.circleci/Dockerfile` (ko's result; each index annotated `io.giantswarm.upstream.version=v0.0.29`, with provenance and an SPDX SBOM), the versions decoupled from upstream's — **the first release of the scheme**, the next major above `0.0.30-gs.5` — and `images.agentgateway` the agentgateway line's own release `agentgateway-upstream/agentgateway:2.0.0`) | v0.0.29 | images `ateapi` `sha256:d99a5316…`, `atecontroller` `sha256:bdb0b08f…`, `atelet` `sha256:0c647af8…`, `atenet` `sha256:44335714…`, `podcertcontroller` `sha256:9df643a6…`, `ateom-gvisor` `sha256:1ea67204…` (linux/amd64 + arm64; each index and its per-platform SPDX attestation signed); charts `substrate` `sha256:291e1b41…`, `substrate-crds` `sha256:99d8211e…` (signed); dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.0.0` (`sha256:63deaa67…`); [CircleCI pipeline 13](https://app.circleci.com/pipelines/github/giantswarm/substrate/13) (pushes 1 min 55 s – 4 min 29 s, `scan` 22 s, `charts` 24 s), every scan clean, every reference `cosign verify`-ed under the pipeline's identity; the orb also moves the images' `latest` tag to the release. The dev build of the same commit is `0.0.0-dev.giantswarm.2026-09-19.13-38-12.h171b9e4` (pipeline 12). Consumers: the agent-platform meta chart's ranges (giantswarm/agent-platform#580), the kagent line's `SUBSTRATE_VERSION` at its re-pin (giantswarm/giantswarm#37872), agentlab's defaults (giantswarm/agentlab#229), retagger's `renamed-substrate.yaml` entries retired (giantswarm/retagger#1238) |
| **1.0.1** (2026-09-22, annotated tag on `2781d80d` = 1.0.0 + #50: PodMonitors for the six workloads that serve metrics (`metrics.podMonitor`, off by default; one object per workload, each pinned to the port name its own pod declares — `prometheus`, `metrics`, `gw-metrics`, `stats` — and the credential provider's to the `substrate.fullname` pod label), with the `metrics.podMonitor.labels` knob the platform sets to the tenant label. Upstream: kagent-dev/substrate#46, open — carried until it merges and the next re-pin drops it. Chart-only and inert until a consumer enables it, so the release is a patch and the agent-platform range `>=1.0.0 <1.1.0` admits it without a re-pin) | v0.0.29 | images `ateapi` `sha256:79296cd2…`, `atecontroller` `sha256:d6c4897e…`, `atelet` `sha256:84936bf9…`, `atenet` `sha256:54ea7ae3…`, `podcertcontroller` `sha256:6d17ba53…`, `ateom-gvisor` `sha256:5f037d42…` (linux/amd64 + arm64); charts `substrate` `sha256:e78db7fe…`, `substrate-crds` `sha256:00554ec9…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.0.0` |
| **1.0.2** (2026-09-23, annotated tag on `a7787e36` on `release-1.0` = 1.0.1 + #60: the agentgateway line's **2.1.1** for atenet-router and atenet-egress (`agentgateway-image` in `.circleci/config.yml` and the chart default `images.agentgateway`): a managed file resource is re-read on every fetch, so the `backendTLS` client certificate of `substrateEgressActorResolution` follows kubelet's PodCertificate rotation instead of expiring in place 24 h after pod start (giantswarm/agentgateway-upstream#20; giantswarm/giantswarm#37915, the 2026-09-23 outage) + the cherry-pick of #58's `podCertificates.maxExpirationSeconds` (`aed0b8d1`; nine sources on this tree, no credential provider). The 1.0.1 egress and router configs validate unchanged against 2.1.1 (`--validate-only`). Cut for the fleet's 1.0.x installations because `giantswarm` is on the beta4 contract; the agent-platform meta chart moves its floor to it together with `substrate.images.agentgateway` (giantswarm/agent-platform#648)) | v0.0.29 | images `ateapi` `sha256:c9edda7f…`, `atecontroller` `sha256:efada927…`, `atelet` `sha256:6f68c35f…`, `atenet` `sha256:a7132403…`, `podcertcontroller` `sha256:4f8f160e…`, `ateom-gvisor` `sha256:f141945d…` (linux/amd64 + arm64); charts `substrate` `sha256:92aa5d9f…`, `substrate-crds` `sha256:bf694053…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.1` (index `sha256:825c2f9e…`); [CircleCI pipeline 52](https://app.circleci.com/pipelines/github/giantswarm/substrate/52), every scan clean |
| **1.0.3** (2026-09-23, annotated tag on `29fd1a12` on `release-1.0` = 1.0.2 + #63: the agentgateway line's **2.1.2** for atenet-router and atenet-egress — the certificate files a Substrate policy's `policies.backendTLS` names, read at parse time and never managed before, become managed dependencies (watched, active, re-read every 60 s), so the client certificate the router's `substrateIngress` and the egress gateway's `substrateEgressActorResolution` present to ate-api follows kubelet's PodCertificate rotation whichever of the pod's two bundles is written first; 2.1.1 followed it only when the podidentity bundle was on disk before the servicedns event's reload (giantswarm/agentgateway-upstream#23; giantswarm/giantswarm#37915). Both 1.0.x gateway configs validate unchanged against 2.1.2 (`--validate-only`); the data plane is the only rendered change. Consumer: the agent-platform meta chart's floor and `substrate.images.agentgateway` (giantswarm/agent-platform#653)) | v0.0.29 | images `ateapi` `sha256:a4c2895e…`, `atecontroller` `sha256:b3f93811…`, `atelet` `sha256:2fce19af…`, `atenet` `sha256:694c788f…`, `podcertcontroller` `sha256:33928dc7…`, `ateom-gvisor` `sha256:61e79663…` (linux/amd64 + arm64); charts `substrate` `sha256:e78f5f2d…`, `substrate-crds` `sha256:f6119ca9…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.2` (index `sha256:a29ec879…`); [CircleCI pipeline 62](https://app.circleci.com/pipelines/github/giantswarm/substrate/62), every scan clean |
| **1.0.4** (2026-09-24, annotated tag on `d60beed9` on `release-1.0` = 1.0.3 + #77: the bundled RustFS Deployment recreates its pod instead of rolling it — one replica on the ReadWriteOnce claim `rustfs-data`, so a rolling update's new pod could not attach the volume the old pod held and the release waiting on it stalled. The 1.1 line fixes the same stall with `RollingUpdate` `maxSurge: 0`/`maxUnavailable: 1` (#83), since `Recreate` is refused by server-side apply over a defaulted rolling strategy on an in-place upgrade. The agent-platform meta chart's 1.0 range `>=1.0.0 <1.1.0` admits it; no consumer follows that range any more) | v0.0.29 | images `ateapi` `sha256:29f18a41…`, `atecontroller` `sha256:dbb013cc…`, `atelet` `sha256:d38133b6…`, `atenet` `sha256:898c3bb1…`, `podcertcontroller` `sha256:c12c1b60…`, `ateom-gvisor` `sha256:481a14cd…` (linux/amd64 + arm64); charts `substrate` `sha256:92c82e25…`, `substrate-crds` `sha256:46df18a7…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.2` (index `sha256:a29ec879…`); [CircleCI pipeline 133](https://app.circleci.com/pipelines/github/giantswarm/substrate/133) |
| **1.1.0** (2026-09-24, tag on `7fd77115` = the 2026-09-24 re-pin of `giantswarm` onto **v0.2.0-beta5** with every carried patch replayed (#73's ledger, nothing dropped; upstream `main` was rewritten onto agent-substrate, so beta5 is not a descendant of beta4 and every commit replayed) + #56: `ActorTemplate.default_egress_policy`, copied onto every actor of the template as its `default` policy, so a golden boot that fetches skills is admitted by atenet (the kagent line sets it from the revision's egress destinations, kagent-upstream#88) + #74: the agentgateway line's **2.2.0** for atenet-router and atenet-egress. **The first release of the beta5 contract**: a minor above 1.0.3 because kagent's ate-api client moves with it (kagent-upstream 1.1.0 stamps this release in `SUBSTRATE_VERSION` and its `go.mod` replace); `release-1.0` stays for the v0.0.29 contract. Cut after the agentlab proof of the dev builds `0.0.0-r588f3d76t20260924131333h6c6d012` (Substrate) and `0.9.11-r588f3d76t20260924141400h1f36e2c` (kagent): `platform-test` 11/11, `a2a-test` 5/5, `skills-test` public 3/3 and private 3/3 with the wrong-token control failing, `agents-test` 6/6 (giantswarm/agent-platform#665, giantswarm/giantswarm#37742)) | v0.2.0-beta5 | images `ateapi` `sha256:07486cc1…`, `atecontroller` `sha256:9260fc1d…`, `atelet` `sha256:293ef348…`, `atenet` `sha256:800c69ee…`, `podcertcontroller` `sha256:b511598f…`, `ateom-gvisor` `sha256:5def3575…` (linux/amd64 + arm64); charts `substrate` `sha256:b110b36a…`, `substrate-crds` `sha256:0c1c999c…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.0` (index `sha256:370827be…`); [CircleCI pipeline 110](https://app.circleci.com/pipelines/github/giantswarm/substrate/110), every scan clean. Consumer: the agent-platform meta chart's ranges `>=1.1.0 <1.2.0` for `substrate`/`substrate-crds` together with kagent 1.1.0 (giantswarm/agent-platform#665) |
| **1.1.1** (2026-09-25, annotated tag on `18e2f1a0` = 1.1.0 + #80: a resume whose golden snapshot is gone is refused as `FailedPrecondition` with the ErrorInfo reason `GOLDEN_SNAPSHOT_UNAVAILABLE` and the metadata `resumable: "false"`, so a conversation started before a runtime upgrade ends at once instead of timing out (giantswarm/substrate#79) + #76 and #83: the bundled RustFS Deployment rolls with `maxSurge: 0` and `maxUnavailable: 1`, its old pod releasing the ReadWriteOnce claim before the new one starts (#83 supersedes #76's `Recreate`, which server-side apply refuses on an in-place upgrade) + #81: the agentgateway line's **2.2.1** for atenet-router and atenet-egress, whose ingress answers a `resumable: false` refusal with the control plane's status at once (giantswarm/agentgateway-upstream#29) + #82: serverboot honours `OTEL_METRICS_EXPORTER=none`. Checked before the tag by a server-side dry-run of the rendered chart as `helm-controller` against a live 1.1.0 release. The agent-platform meta chart's ranges `>=1.1.0 <1.2.0` admit it without a re-pin; the worker image stays the kagent line's stamp) | v0.2.0-beta5 | images `ateapi` `sha256:5e911cd5…`, `atecontroller` `sha256:c6435fa5…`, `atelet` `sha256:0163ef4b…`, `atenet` `sha256:80eac231…`, `podcertcontroller` `sha256:385096b2…`, `ateom-gvisor` `sha256:574c9106…`, `kubernetes-secrets` `sha256:6da901b4…` (linux/amd64 + arm64); charts `substrate` `sha256:c58ae6fe…`, `substrate-crds` `sha256:0b5aaffb…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` (index `sha256:b3decd06…`); [CircleCI pipeline 144](https://app.circleci.com/pipelines/github/giantswarm/substrate/144) |
| **1.1.2** (2026-09-27, annotated tag on `74036f9a` = 1.1.1 + #85: the `rustfs-bucket-init` Job is a Helm hook (`post-install,post-upgrade`, `before-hook-creation,hook-succeeded`), recreated on every install and upgrade, and `images.awsCli` defaults to the gsoci copy `gsoci.azurecr.io/giantswarm/aws-cli:2.17.0`, so the Job's image can move without an upgrade failing on its immutable pod template (giantswarm/agent-platform#580). Proven before the merge on an agentlab through helm-controller, 1.1.1 → the patched chart (old Job deleted, hook Job from gsoci completed and removed) and back (plain Job recreated, no conflict); after the tag, an agentlab upgrade from agent-platform 4.86.0 to the meta chart that forwards the gsoci value (agent-platform#716, released in 4.87.0 with the ranges `>=1.1.2 <1.2.0`)) | v0.2.0-beta5 | images `ateapi` `sha256:5ec924a8…`, `atecontroller` `sha256:8731d518…`, `atelet` `sha256:a2a200e6…`, `atenet` `sha256:07823948…`, `podcertcontroller` `sha256:c63a3f80…`, `ateom-gvisor` `sha256:82c7ad5c…`, `kubernetes-secrets` `sha256:bed77a31…` (linux/amd64 + arm64); charts `substrate` `sha256:a3c2ff4d…`, `substrate-crds` `sha256:d83561a1…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.1.3** (2026-09-28, annotated tag on `b23604fa` = 1.1.2 + #87: `atelet.priorityClass.preemptionPolicy`, `PreemptLowerPriority` by default (unchanged) or `Never`, which renders the class `ate-node-critical-non-preempting` because a PriorityClass's `preemptionPolicy` is immutable; with `Never` atelet keeps its priority and evicts nothing (giantswarm/agent-platform#731). Proven before the tag on an agentlab through helm-controller on the dev build `1.1.3-r588f3d76t20260928113438hb23604f`: 1.1.2 → `Never` (the old class deleted, atelet rolled onto the new one); a node filled by a low-priority pod keeps it, atelet Pending with `preemption: not eligible due to preemptionPolicy=Never` and Ready once room appears, where 1.1.2 evicted the filler; its consumer is giantswarm/agent-platform#734 (the ranges `>=1.1.3 <1.2.0`, `Never`)) | v0.2.0-beta5 | images `ateapi` `sha256:727defec…`, `atecontroller` `sha256:b454f3ef…`, `atelet` `sha256:bceab5dc…`, `atenet` `sha256:6e22635f…`, `podcertcontroller` `sha256:01d45a46…`, `ateom-gvisor` `sha256:1bbc306f…`, `kubernetes-secrets` `sha256:0fc94b3b…` (linux/amd64 + arm64); charts `substrate` `sha256:359bd890…`, `substrate-crds` `sha256:4933664f…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.1.4** (2026-09-29, tag on `1565be1b` = 1.1.3 + #90: the durable pause copy kept out of `external_snapshot` + #92: `atenetEgress.upstream` extra CAs for the egress gateway's upstream TLS + #93: `otlploggrpc` v0.21.0 for GO-2026-6508, with the ledger rows #88 and #91. Not published under this version: gsoci holds no `1.1.4` image or chart, and the same commit is released as 1.2.0) | v0.2.0-beta5 | none published |
| **1.2.0** (2026-09-29, tag on `1565be1b` = 1.1.3 + #90: the durable pause copy kept out of `external_snapshot` + #92: extra CAs for the egress gateway's upstream TLS (`atenetEgress.upstream`, a new chart value, hence the minor) + #93: `otlploggrpc` v0.21.0 for GO-2026-6508, with the ledger rows #88 and #91) | v0.2.0-beta5 | images `ateapi` `sha256:2537c6c6…`, `atecontroller` `sha256:d414254b…`, `atelet` `sha256:e683193e…`, `atenet` `sha256:2cee6ba5…`, `podcertcontroller` `sha256:932fcda0…`, `ateom-gvisor` `sha256:49ff7905…`, `kubernetes-secrets` `sha256:50f4248b…`; charts `substrate` `sha256:59b1cbd0…`, `substrate-crds` `sha256:24af0d23…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.0** (2026-09-30, tag on `2b76e00a` = 1.2.0 + #98: `WorkerPool.spec.template` takes `podAffinity`, so a worker schedules only where atelet runs, with #96 and #97, the platform-alignment files and the boilerplate check's skip of the aligned `cliff.toml`) | v0.2.0-beta5 | images `ateapi` `sha256:2989cb90…`, `atecontroller` `sha256:46637f66…`, `atelet` `sha256:33e2278d…`, `atenet` `sha256:d92456bd…`, `podcertcontroller` `sha256:cab266bc…`, `ateom-gvisor` `sha256:609b6cfb…`, `kubernetes-secrets` `sha256:18259a0c…`; charts `substrate` `sha256:e76e9bac…`, `substrate-crds` `sha256:a4080209…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.1** (2026-10-06, tag on `577984a8` on `release-1.3` = 1.3.0 + #131: the ActorTemplate reconciler replaces a golden actor that crashed before its snapshot instead of failing the template, counted in `GoldenSnapshotStatus.golden_actor_crashes`, failing past five + #133: ateom's child reaper collects orphans while tracked runsc subcommands run, so a `runsc delete` no longer deadlocks on a reaped child; giantswarm/substrate#105) | v0.2.0-beta5 | images `ateapi` `sha256:385f3dd6…`, `atecontroller` `sha256:45791a10…`, `atelet` `sha256:869398ae…`, `atenet` `sha256:e81949ca…`, `podcertcontroller` `sha256:a02dd302…`, `ateom-gvisor` `sha256:aeea0341…`, `kubernetes-secrets` `sha256:c5d0a48e…`; charts `substrate` `sha256:fac71d0a…`, `substrate-crds` `sha256:cd2c4eac…` |
| **1.3.2** (2026-10-07, tag on `c7cae9e0` on `release-1.3` = 1.3.1 + #141: the egress gateway's agentgateway config has no `protocol: TCP` listener, so a tunnel from an actor whose first bytes are neither a TLS ClientHello nor an HTTP request is closed at the internal `AUTO` bind instead of being forwarded to its CONNECT authority without the egress policy; giantswarm/substrate#140) | v0.2.0-beta5 | images `ateapi` `sha256:77f09bbe…`, `atecontroller` `sha256:efa25dea…`, `atelet` `sha256:cf80858e…`, `atenet` `sha256:acab53fb…`, `podcertcontroller` `sha256:6c852bf9…`, `ateom-gvisor` `sha256:1351ec48…`, `kubernetes-secrets` `sha256:f675d60c…`; charts `substrate` `sha256:48407de2…`, `substrate-crds` `sha256:3cd19781…` |
| **1.3.3** (2026-10-07, tag on `28d7fc02` on `release-1.3` = 1.3.2 + #149: a suspended actor may be repointed at a template that adds, removes or changes `system_info` volumes, with the ledger rows #143) | v0.2.0-beta5 | images `ateapi` `sha256:2da12c4e…`, `atecontroller` `sha256:fea747f9…`, `atelet` `sha256:3fee847d…`, `atenet` `sha256:375766cf…`, `podcertcontroller` `sha256:f8830620…`, `ateom-gvisor` `sha256:6a0f62ca…`, `kubernetes-secrets` `sha256:27341003…`; charts `substrate` `sha256:3e9ec812…`, `substrate-crds` `sha256:5c7fc52f…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.4** (2026-10-07, tag on `9024e3eb` on `release-1.3` = 1.3.3 + #152: the ActorTemplate reconciler migrates a golden snapshot status recorded before golden tags into the current shape) | v0.2.0-beta5 | images `ateapi` `sha256:f08f31bf…`, `atecontroller` `sha256:1ccabd01…`, `atelet` `sha256:b286c110…`, `atenet` `sha256:8dfbc8c1…`, `podcertcontroller` `sha256:3f6a2f80…`, `ateom-gvisor` `sha256:296ba007…`, `kubernetes-secrets` `sha256:626034ac…`; charts `substrate` `sha256:cf3189aa…`, `substrate-crds` `sha256:88749a06…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.5** (2026-10-07, tag on `68409a86` on `release-1.3` = 1.3.4 + #161: ate-api reports its egress-policy contract and `kubectl ate` checks it + #156: `kubectl ate get`, `create` and `update egress-policy`, with the ledger row #155) | v0.2.0-beta5 | images `ateapi` `sha256:ed764214…`, `atecontroller` `sha256:6f5cac61…`, `atelet` `sha256:a897e9c8…`, `atenet` `sha256:86d4d146…`, `podcertcontroller` `sha256:62d24983…`, `ateom-gvisor` `sha256:23c91497…`, `kubernetes-secrets` `sha256:a073a470…`; charts `substrate` `sha256:4c844a31…`, `substrate-crds` `sha256:d03f339d…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.6** (2026-10-07, tag on `d9c5be30` on `release-1.3` = 1.3.5 + #167: atelet reads its pinned images from a file instead of the pod template + #169: `DeleteActorTemplate` deletes the template row before its golden actor and tag + #173: the e2e suite that restarts atelet under a running actor + #174 and #180: the line's auto-release tags `release-1.3` backports as 1.3.x patches and validate skips the tagged maintenance branches) | v0.2.0-beta5 | images `ateapi` `sha256:b1f8eccf…`, `atecontroller` `sha256:e0e3ec22…`, `atelet` `sha256:8fbc2b43…`, `atenet` `sha256:7d98a7b2…`, `podcertcontroller` `sha256:84c55c6b…`, `ateom-gvisor` `sha256:960458c6…`, `kubernetes-secrets` `sha256:143de9d9…`; charts `substrate` `sha256:941ea5aa…`, `substrate-crds` `sha256:b6360058…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.3.7** (2026-10-08, tag on `42230a56` on `release-1.3` = 1.3.6 + #184: Pause, Suspend and Resume take an optional fencing token + #196: ate-api no longer logs the raw PostgreSQL connection string at startup) | v0.2.0-beta5 | images `ateapi` `sha256:49c9b119…`, `atecontroller` `sha256:2c7ccef1…`, `atelet` `sha256:9923f966…`, `atenet` `sha256:a2ba605e…`, `podcertcontroller` `sha256:48a735eb…`, `ateom-gvisor` `sha256:1eadfaf1…`, `kubernetes-secrets` `sha256:d56ccc88…`; charts `substrate` `sha256:ce459e20…`, `substrate-crds` `sha256:a60ccd1d…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.4.0** (2026-10-05, tag on `e27ae953` = the 2026-10-05 re-pin of `giantswarm` onto kagent-dev/substrate **v0.3.0-alpha3** with the carried patches replayed (giantswarm/substrate#119; alpha3 was tagged off a side branch, so the merge base with upstream `main` is `2d5ef32d`) and the agentgateway line's **2.3.0** for atenet-router and atenet-egress. Promoted from `v1.4.0-rc.8`, created by hand because the release workflow counted only the candidates reachable from the rebased branch and tried `rc.1` again (fixed by giantswarm/devctl#2498); `rc.1` to `rc.7` are the v0.2.0-beta5 contract and are not in this release. **The first release of the alpha3 contract**: a minor because kagent's ate-api client moves with it (kagent-upstream 1.3.0 stamps it). ate-api cannot migrate a database of the 1.3 line (`relation "tuple" already exists` at migration 2), so an installation moves on an empty `substrate` schema. Consumer: the agent-platform meta chart's ranges `>=1.4.0 <1.5.0` with kagent `>=1.3.0 <1.4.0`; graveler and glean run it since 2026-10-06) | v0.3.0-alpha3 | images `ateapi` `sha256:60430acf…`, `atecontroller` `sha256:c8b79217…`, `atelet` `sha256:5b3f188d…`, `atenet` `sha256:551d0ef8…`, `podcertcontroller` `sha256:68bfe120…`, `ateom-gvisor` `sha256:da725b1d…`, `kubernetes-secrets` `sha256:ebe29da7…`; charts `substrate` `sha256:39fb827a…`, `substrate-crds` `sha256:495e6637…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0`; [CircleCI pipeline 270](https://app.circleci.com/pipelines/github/giantswarm/substrate/270) |
| **1.5.0** (2026-10-08, tag on `314cbe4f` = 1.4.0 + the 1.3.x backports on the 1.4 line (#135 the child reaper, #136 no OTLP export for a signal without an endpoint, #138 the OpenFGA schema adoption, #148 the `system_info` volume repoint, #151 the golden status migration, #160 the egress-policy contract, #166 the pinned images file, #168 the template delete order, #182 the lease fencing token) + #128: no raw PostgreSQL connection string in the startup log + #129: the semantic conventions of the SDK's own detectors + #165: the carried snapshot source field's own number + #186: the tracked PIDs shared across every reaper + #193: the durable pause's background commit off the actor's lease, with the e2e, CI and sync fixes #176, #179, #181, #185, #188, #191 and #192 and the ledger rows) | v0.3.0-alpha3 | images `ateapi` `sha256:64823dac…`, `atecontroller` `sha256:1e34b1ae…`, `atelet` `sha256:3a57e03d…`, `atenet` `sha256:93f77b66…`, `podcertcontroller` `sha256:79e25ed2…`, `ateom-gvisor` `sha256:7e7aca1a…`, `kubernetes-secrets` `sha256:95d8a00b…`; charts `substrate` `sha256:f2d2c4ea…`, `substrate-crds` `sha256:c318a153…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0` |
| **1.6.0** (2026-10-08, tag on `85d02ece` = the 2026-10-08 re-pin of `giantswarm` onto kagent-dev/substrate **v0.4.0-alpha1** with the carried patches replayed (#210; the agentgateway line's 2.3.0 unchanged). **The first release of the v0.4.0-alpha1 contract**: the AccessPolicy RPCs, `ActorJWTSource` and dual-stack addresses; the PostgreSQL schema gains upstream's additive `000003_access_policies.sql`, so a 1.5.x database upgrades in place; on its first start ate-api-server moves the line's own lease-holder migration into its own ledger `fork_schema_migrations` (#210), and it runs `SET ROLE` on every connection, so `postgres.readWriteRole` and `ownerRole` must name roles the DSN's user may assume, the chart's default being `postgres`) | v0.4.0-alpha1 | images `ateapi` `sha256:c0464521…`, `atecontroller` `sha256:e1972b3d…`, `atelet` `sha256:9c410cdb…`, `atenet` `sha256:93b31a72…`, `podcertcontroller` `sha256:b3838bd0…`, `ateom-gvisor` `sha256:2b388139…`, `kubernetes-secrets` `sha256:b6eb2dbc…`; charts `substrate` `sha256:1dada79f…`, `substrate-crds` `sha256:66dcc21e…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0` |
| **1.6.1** (2026-10-09, tag on `315b9ff5` = 1.6.0 + the sandbox's resolv.conf keeping only the cluster's search domains, the gVisor and microVM ateom both (#216), and Go 1.27.2 with `golang.org/x/net` v0.60.0 (#214)) | v0.4.0-alpha1 | images `ateapi` `sha256:72a4bf2e…`, `atecontroller` `sha256:44fe482d…`, `atelet` `sha256:c2e66b0f…`, `atenet` `sha256:8c1c4dcf…`, `podcertcontroller` `sha256:75a635b4…`, `ateom-gvisor` `sha256:c9631f21…`, `kubernetes-secrets` `sha256:8094b9ea…`; charts `substrate` `sha256:e96c124e…`, `substrate-crds` `sha256:c7ca8ab6…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0` |
| **1.6.2** (2026-10-09, tag on `07c1474c` = 1.6.1 + the egress ext-proc's telemetry named `atenet-egress` instead of `atenet-router` (#217)) | v0.4.0-alpha1 | images `ateapi` `sha256:2d4460b6…`, `atecontroller` `sha256:6a87f26b…`, `atelet` `sha256:5229bee6…`, `atenet` `sha256:05a857a9…`, `podcertcontroller` `sha256:f458ee1d…`, `ateom-gvisor` `sha256:e170ef02…`, `kubernetes-secrets` `sha256:b2227fde…`; charts `substrate` `sha256:c05fc0b9…`, `substrate-crds` `sha256:e22bfc51…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0` |
| **1.6.3** (2026-10-09, tag on `e650362f` = 1.6.2 + the gVisor SandboxConfig's pause image as the chart value `images.pause`, defaulting to its gsoci copy at upstream's digest (#225), and the platform-standard file alignment (#226)) | v0.4.0-alpha1 | images `ateapi` `sha256:bf4b2d46…`, `atecontroller` `sha256:b97599f6…`, `atelet` `sha256:10c609cd…`, `atenet` `sha256:3914a556…`, `podcertcontroller` `sha256:1229f718…`, `ateom-gvisor` `sha256:6784f03e…`, `kubernetes-secrets` `sha256:4b39d82a…`; charts `substrate` `sha256:85ec6f7c…`, `substrate-crds` `sha256:953e7017…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0` |
| **1.7.0** (2026-10-10, tag on `90654619` = 1.6.3 + an existing volume mounted per actor at a sub-path and repeated message fields compared with `proto.Equal` (#234), a missing read-write `sub_path` created at the bind (#239), Docker Hub images of the line's CI pulled through `mirror.gcr.io` (#242), and the actor's hostname in its `/etc/hosts` (#243)) | v0.4.0-alpha1 | images `ateapi` `sha256:06d0a3be…`, `atecontroller` `sha256:a3368f0f…`, `atelet` `sha256:7a630a40…`, `atenet` `sha256:04fdaf5a…`, `podcertcontroller` `sha256:9efe7585…`, `ateom-gvisor` `sha256:917bef78…`, `kubernetes-secrets` `sha256:fc7ad9bd…`; charts `substrate` `sha256:9b542c50…`, `substrate-crds` `sha256:06d157e5…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.3.0`; [pipeline 567](https://app.circleci.com/pipelines/gh/giantswarm/substrate/567), `scan` green |

**Scans.** Every own image is scanned with Trivy (HIGH and CRITICAL, fixable only) after the push and before the
charts that reference it are published. A fixable finding fails the publish: bump the module (upstream first) or,
when upstream has no fix, add a time-boxed entry to `.trivyignore` (`CVE-… exp:YYYY-MM-DD # reason, tracking
issue`) — an expired entry fails again and is re-triaged, not extended. The agentgateway image is scanned
report-only here: it is the agentgateway line's build, its scan gates its own publish and a finding is fixed there. `govulncheck` covers the Go module graph on every push, pull
request and weekly. The line runs no nancy: `govulncheck` is its Go dependency scan, required on every pull request
and on a re-pin's landing, so every commit a release tag points at passed it, and it reports only vulnerabilities
the code reaches, where nancy would flag every module version with a second ignore file to keep; the
`.nancy-ignore.generated` the repository alignment writes here is read by nothing, as on the kagent and
agentgateway lines.

## Consumers

Each consumer moves to the decoupled versions (`1.0.0` and up, "Versions") in its own change: the agent-platform
meta chart's ranges (giantswarm/agent-platform#580), the kagent line's `SUBSTRATE_VERSION` at its re-pin
(giantswarm/giantswarm#37872), agentlab's defaults (giantswarm/agentlab#229), and retagger's
`renamed-substrate.yaml` entries, retired (giantswarm/retagger#1238).

| Consumer | Where the pin lives | Selects |
|---|---|---|
| [agentlab](https://github.com/giantswarm/agentlab) | `internal/lab/substrate.go` (`substrateChartsRepo`, `substrateImageRegistry`, `substrateVersion`) | an exact dev version or release; installs `substrate-crds` + `substrate` and preloads the worker image |
| agent-platform meta chart 4.0 (`components.substrate` / `components.substrate-crds`, the `substrate:` values block; [giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342)) | `helm/agent-platform/values.yaml`: the two components' version ranges and `kagent.substrateWorkerPool.workerImage` | the `substrate-crds` + `substrate` charts by release range (`>=1.0.0 <2.0.0`) or at an exact dev version, and the `ateom-gvisor` image at the same version (the `WorkerPool` the kagent chart renders) |
| [giantswarm/kagent-upstream](https://github.com/giantswarm/kagent-upstream) | `Makefile` `SUBSTRATE_REPO ?= oci://gsoci.azurecr.io/giantswarm/substrate/helm` (the kagent line follows the path with its own move to gsoci, giantswarm/giantswarm#37872), `SUBSTRATE_VERSION` | the `substrate`/`substrate-crds` chart dependencies of the kagent charts (off in the platform, which installs Substrate as cluster infrastructure) |

## Assets that are not images

- **gVisor `runsc`**: the chart's `SandboxConfig gvisor-default` (`charts/substrate/templates/sandboxconfig-gvisor.yaml`)
  names `gs://gvisor/releases/release/20260803/{x86_64,aarch64}/gvisor.tar.bz2`; atelet downloads the release
  tarball at prewarm. Unchanged from upstream and not mirrored; a cluster needs egress to
  `storage.googleapis.com` from the atelet pods (or an override of `spec.assets` on the SandboxConfig).
- **micro-VM assets** (kata, cloud-hypervisor, virtiofsd): assembled by `hack/microvm-assets/assemble.sh` for the
  e2e suites; the platform does not run the micro-VM sandbox class.

## Contributing

- **Upstream first.** Every behavioural change is a pull request to
  [kagent-dev/substrate](https://github.com/kagent-dev/substrate) (issues are disabled there; discussion goes
  through the pull request or the kagent community channels) with DCO sign-off (`git commit -s`); pull-request
  workflows from forks wait for a maintainer's approval. The line carries the same change as a
  `git cherry-pick -x` of the upstream commit (or, before merge, of your pull-request branch) until an upstream
  release contains it, with a row in #37742.
- **Fork-only changes** (workflows, this file): a pull request from `fork/<topic>` against `giantswarm`, squash-merged.
  A cherry-pick of an upstream commit is rebase-merged (see "Branches").
- **The ledger**: a pull request against a line that changes more than this file names its own number as `#n` in a
  ledger file of its own (`fork/<change>.md`, or `fork/fork-infrastructure-*.md` for CI and tooling; a maintenance
  branch keeps its own copy of this file as its ledger), or
  carries the label `no-ledger` with a line `no-ledger: <reason>` in its description. The `fork-ledger` workflow
  checks it on every pull request (`docs(fork)` pull requests, re-pin candidates and bot pull requests need no
  ledger entry) and, on a maintenance branch, that every pull request merged into it has its row in the branch's copy; it
  names each missing number.
- **Experiments**: your own personal fork. Branches here exist to become pull requests.
- **What CI runs on a pull request**: upstream's `pr-workflow` (unit, root-gated and e2e suites on kind, gVisor and
  micro-VM lanes), `helm-e2e` (the charts on kind), `govulncheck` and `fork-ledger` (the ledger, above); `run-tests` and `govulncheck` are required. `govulncheck` runs once per Go
  module (upstream's matrix since v0.4.0-alpha1) and a job of the line reports the one `govulncheck` status the
  rulesets and the land step require.
  The CircleCI `publish` workflow runs on every push (a pull request's head publishes its own dev build) and on tags.
- Upstream's `release.yaml` (dispatch-only; it would have pushed images and charts to ghcr.io under an arbitrary
  tag) and `release-notes.yaml` (it drafts a GitHub release with upstream's notes on every `v*` tag, which
  `zz_generated.auto_release` owns here) are **removed** on this branch: nothing publishes from GitHub Actions. Do not push tags other than
  `vX.Y.Z-gs.N` releases.
