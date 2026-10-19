# The Giant Swarm line of Agent Substrate

This repository is Team Bumblebee's fork of [kagent-dev/substrate](https://github.com/kagent-dev/substrate)
(itself a fork of [agent-substrate/substrate](https://github.com/agent-substrate/substrate)). The Giant Swarm
Agent Platform's kagent (API v2) runs every agent as a Substrate actor; this fork is where the platform's
Substrate is pinned, built, scanned and published. It answers one question from one place: **which Substrate
are we running, and why does it differ from upstream?**

Tracking: each carried patch is a ledger file in `fork/` that
names its pull request here and its upstream pull request or issue; the line's own work is tracked in this repository's
public issues. The kagent line is built the same way ([giantswarm/kagent-upstream](https://github.com/giantswarm/kagent-upstream)).
Giant Swarm's internal planning stays out of this public file: where a row once cited an internal issue, it states the
reasoning instead.

## Branches

| Branch | What it is | Who moves it |
|---|---|---|
| `main` | A pure mirror of upstream `main`. Upstream rebases its `main` onto agent-substrate (no release tag is an ancestor of it), so the mirror is a **forced** update. Never edited, never the target of a pull request. The mirrored commits carry upstream's workflow files, whose `main` triggers would run upstream's suites here for nothing — the sync cancels those runs right after the push. | the sync workflow (as the HeraldBot App) — the `protect-main` ruleset admits the App and nobody else; a repair is a `workflow_dispatch` of the sync |
| `giantswarm` (default) | **The line**: the upstream release tag the platform's kagent pins ("the pin") + cherry-picked upstream fixes + this fork's own files. Every change of the fork's own files is a pull request against it. Merge method: a cherry-pick of an upstream commit is **rebase-merged**, one commit per patch, so its patch identity survives and `git rebase` drops it by itself once the pin contains it; fork-infrastructure pull requests are squashed. Edits of this file alone (`docs(fork)`) are merged the same way between re-pins, and a re-pin folds every one of them into the single ledger commit at the top of the replayed stack, so the line carries one commit per change and not one per ledger edit (since 2026-10-05; the pre-fold history stays reachable from the release tags). | pull requests (`run-tests` and `govulncheck` required); a re-pin lands only as a candidate's head those checks passed on, pushed by the sync workflow's `land` (the HeraldBot App) — never through a merge or `devctl pr merge`, never by hand |
| `fork/<topic>` | pull-request branches against `giantswarm` | anyone in the team |
| `release-1.0` | **The 1.0 line** for installations whose kagent speaks the v0.0.29 ate-api contract (kagent-upstream 1.0.x): `v1.0.1` plus what a 1.0.x patch needs, cut when `giantswarm` has already moved to a newer pin (it moved to v0.2.0-beta4 on 2026-09-22, a contract change the kagent line moves with). Pull requests against it are rebase-merged like the line's; its releases are `vX.Y.Z` tags on it, published by the same pipeline. Its ledger is this file on `giantswarm`; the branch's own copy carries the same rows for the tagged tree. Retired once the fleet's kagent is past the v0.0.29 contract. | pull requests; the same suites as `giantswarm` run on a pull request against it |
| `release-1.3` | **The 1.3 line** for installations on the meta chart's Substrate range `>=1.3.0 <1.4.0` once `giantswarm` moved to the v0.3.0-alpha3 pin (1.4.0): `v1.3.0` plus what a 1.3.x patch needs. Pull requests against it are rebase-merged; its releases are `v1.3.Z` tags on it, published by the same pipeline. Its ledger is this file on `giantswarm`; the branch's own copy carries the same rows for the tagged tree. Retired once the fleet runs 1.4. | pull requests |
| `release-1.9` | **The 1.9 line** for installations stepping from 1.8 while `giantswarm` moved to 1.10 (the egress contract's credential path prefix): `v1.9.1-rc.5` plus what a 1.9.x patch needs. Pull requests against it are rebase-merged; its releases are `v1.9.Z` tags on it, published by the same pipeline. Its ledger is this file on `giantswarm`; the branch's own copy carries the same rows for the tagged tree. Retired once the fleet's consumers speak the 1.10 egress contract. | pull requests |
| `sync/<date>-<pin>` | re-pin candidates (`sync/<date>-<line>-<pin>` for a maintenance line): the new pin + the replayed carried patches, each with a pull request against its line that is reviewed and closed by the landing, never merged; the landing deletes the branch, and the next re-pin's sync deletes every candidate it supersedes (any `sync/*` branch no other line's open pull request holds) | the sync workflow; a human finishes one whose replay conflicted |

## Pin

| | |
|---|---|
| Upstream tag | **v0.5.0-alpha2** (2026-10-08; tag commit `9c4b1fb5`, "Let every Helm chart container set resource requests and limits (#54)"; 109 commits past v0.4.0-alpha1, which is not its ancestor, so the re-pin replays every carried commit). Previous pins: v0.0.26 (2026-09-10 → 2026-09-14, releases `v0.0.27-gs.1`–`gs.9`), v0.0.29 (2026-09-14 → 2026-09-22, releases `v0.0.30-gs.1`–`gs.5`, `1.0.0`, `1.0.1`; `release-1.0` stays on it), v0.2.0-beta4 (2026-09-22 → 2026-09-24, dev builds only), v0.2.0-beta5 (2026-09-24 → 2026-10-05, releases `1.1.0`–`1.3.0` and the `1.4.0` candidates; `1.4.0` is cut from `81abb52e`, the last candidate before the re-pin), v0.3.0-alpha3 (2026-10-05 → 2026-10-08, releases `1.4.0`–`1.5.x`), v0.4.0-alpha1 (2026-10-08 → 2026-10-12, releases `1.6.0`–`1.8.x`) |
| Why this one | upstream kagent-dev/kagent pins `github.com/kagent-dev/substrate v0.5.0-alpha2` in `go/go.mod` (the `replace` of `github.com/agent-substrate/substrate`) since kagent-dev/kagent#3116 (`5eb9b633`, "Bump Substrate to 0.5.0-alpha2"), and at `main` `92dd6caa` (2026-10-11), the head the kagent line re-pins onto; `giantswarm/kagent-upstream` replaces the same path with this line's release. Since kagent-dev/kagent#2708 kagent sets up its PostgreSQL through `pkg/postgressetup`, which v0.5.0-alpha2 exports. v0.5.0-alpha2 changes the contracts between the components: `SetWorkerCapacity` is `RegisterWorker` on `WorkerService` and `AteomSupport`, with the worker's `HardwareIdentity` (agent-substrate/substrate#2282); `MintActorJWTRequest.actor_uid` is gone (#2207); `SnapshotConfig.on_pause` is gone and a pause captures the `on_commit` scope (#2309); an actor's node is `ActorStatus.assigned_node`, and `LocalSnapshot.content_scope` moves onto field 2, where the line's node list was; the control API answers through `internal/apierror`. The persisted Actor rows of a 1.8 release are migrated on read (see its ledger file), the PostgreSQL schema gains nothing, so the move upgrades a 1.8.x installation in place. During the roll an ateom of the old release retries its capacity report against the renamed RPC until its pool rolls; the workers it registered keep their capacity. The ate-api gRPC contract between kagent's client and Substrate's server must match, so the two lines move together (2026-09-22 onto beta4, 2026-09-24 onto beta5, 2026-10-05 onto alpha3, 2026-10-08 onto v0.4.0-alpha1, 2026-10-12 onto v0.5.0-alpha2). |
| agentgateway it runs | the `agentgateway-image` parameter of `.circleci/config.yml`, equal to the chart default `images.agentgateway` (the publish refuses a drift — the chart must install unstamped, and its egress config is written for this build's schema) and to the image, by digest, of `manifests/ate-install/components/agentgateway`, which the e2e installs (`hack/verify/agentgateway-image.sh` fails a pull request on any drift; before the 2026-10-08 re-pin that component ran upstream's alpha build, so the e2e never ran the line's agentgateway): a release of the agentgateway line, published by that line from CircleCI under its own gsoci path and versioned on its own — **`gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.5.0-rc.2`** (tag `v2.5.0-rc.2` on `46019685` = 2.5.0-rc.1 + the Substrate egress applying a path-prefixed credential header to a path with an encoded slash (`%2F` read as a separator yields no dot or empty segment), giantswarm/agentgateway-upstream#106 for giantswarm/agentgateway-upstream#105, the rule this line's `egresspolicy.ReplaceHeadersFor` follows since #483; pinned here by #485; before it 2.5.0-rc.1, tag `v2.5.0-rc.1` on `76f469bc` = 2.4.0 + the Substrate egress relaying TCP a `tcp` egress rule allows, decided on the address and port the actor connected to, giantswarm/agentgateway-upstream#101 for giantswarm/substrate#164, and the agentgateway line's carried fixes since: a stateful MCP session's pin to a gone endpoint dropped, test isolation of the AWS SDK config and the proxy test configs; pinned here by the pull request that adds the `tcp` rule; before it 2.4.0, tag `v2.4.0` on `d117e601`, the stable of `v2.4.0-rc.2` on the same commit = 2.3.6 + the Substrate egress injecting an `actor_jwt` entry of `replace_headers` with a JWT minted through `Control.MintActorJWT`, giantswarm/agentgateway-upstream#80 for giantswarm/agentgateway-upstream#79 and #82, which calls the mint by the method name Substrate serves (2.4.0-rc.1 asked for `MintActorJwt` and was answered `Unimplemented`), the agentgateway dataplane's half of the actor-JWT injection this line carries (its ledger file); pinned here as the stable 2.4.0 by #429, after #421 pinned its release candidate; before it 2.3.6, tag `v2.3.6` on `df3072c3` = 2.3.5 + a request the upstream refused before processing it (a refused HTTP/2 stream, or one above the upstream's `GOAWAY`) retried once on a fresh connection, giantswarm/agentgateway-upstream#78 for #401, without which an actor's `git-upload-pack` to github.com was answered `503` when a pooled connection the upstream was closing refused its stream, and the JWKS refetch awaiting the refresh in flight; proven as the egress of 1.8.5 in agentlab (`skills-test`, `agents-test`); pinned here by #411; before it 2.3.5, tag `v2.3.5` on `d5987a97` = 2.3.0 + the agentgateway line's carried patches since: the JWKS refetch on an unknown key id, the gRPC status of a trailers-only refusal logged, Go 1.27.2 and `golang.org/x/net` v0.61.0, and the Substrate egress applying a `replace_headers` entry by its `path_prefix` (giantswarm/agentgateway-upstream#65 for giantswarm/agentgateway-upstream#62), which the path-scoped credentials of this line's egress policy need on the agentgateway dataplane; pinned here by #355; before it 2.3.0, tag `v2.3.0` on `97b142d6` = the agentgateway line's 2026-10-05 re-pin onto upstream v1.6.0 (`ea560864`) plus its 40 carried commits; v1.6.0 contains agentgateway#3677 (`8d6be5fb`), the egress gateway authorizing from the `ateom-for-actor` SPIFFE URI of agent-substrate/substrate#1809 that alpha3's ateom presents and 2.2.2 refuses, and agentgateway#3689 (`c4143926`), the squash of `50999825`, the build upstream's v0.4.0-alpha1 chart pins as `ghcr.io/kagent-dev/substrate/agentgateway:50999825cb55` (alpha1 commit `50e20d5e`), so the v0.4.0-alpha1 re-pin needs no new agentgateway release; pinned here by `a07cc154`; before it 2.2.2, tag `v2.2.2` on `149d9b40` = 2.2.1 + an egress policy call that never reached its policy service answered `503 unavailable` instead of `403 denied`, giantswarm/agentgateway-upstream#35, − the named buffer error on an MCP upstream's response, reverted by giantswarm/agentgateway-upstream#36; 2.2.1 is tag `v2.2.1` on `13325b82` = 2.2.0 + a resume refusal the control plane marks not resumable answered at once with its own status, giantswarm/agentgateway-upstream#29, which the golden refusal of #80 needs; 2.2.0 is tag `v2.2.0` on `71f1adf6`, the agentgateway line's 2026-09-24 re-pin onto upstream `main` @ `3528a428` plus its carried patches and the grpc pin; before it `2.1.2`, tag `v2.1.2` on `39023841`), which descends from the dev build the 2026-09-22 re-pin chose (`0.0.0-dev.giantswarm.2026-09-22.11-31-30.h6afcce9` = upstream agentgateway `main` @ `08f4563b`, 2026-09-21, ≥ `8dba3989`: #3411's egress credential injection and #3524's provider contract, which beta4's and beta5's charts pin as `ghcr.io/agentgateway/agentgateway:v0.0.0-alpha.8dba3989`) plus the line's carried patches; the agentgateway line moved to `main` @ `3528a428` on 2026-09-24 without a release yet, so this line ran 2.1.2 until that release, `2.2.0`, existed (a released build, as the platform's image policy requires). Since 2.1.1 a file resource is read from disk on every managed fetch, so an egress CA switch reloads `tls.crt` and `tls.key` as one pair: the dev build and upstream's alpha reload them as two events and serve leaves signed with the other file's key in between, which fails every handshake of that window. Since 2.1.2 the files a parse reads are watched as well. `release-1.0` pins the same release. v0.5.0-alpha2's chart pins upstream's nightly `cr.agentgateway.dev/agentgateway:v0.0.0-alpha.168b05d6` (agent-substrate/substrate#2158) for the TLS passthrough listener and the credential providers on the HTTP and HTTPS egress policies, which the line's chart has rendered for 2.3.0 since before the pin, so the line stays on 2.3.0. |
| When it moves | only together with kagent's pin, proven in agentlab first (`agentlab configure --defaults --chart-branch poc/kagent-main && agentlab up` and the proofs) — see "Re-pin". Not on a schedule. |
| Derived how | the nearest upstream `v*` tag reachable from the line, prereleases included (`git tag --merged` against `git ls-remote --tags upstream`, nearest by commit distance — a version sort would place a prerelease behind its release): the line's own tags version the package (see "Versions") and share the `v*` namespace, so they never count — and upstream `main`, rebased onto agent-substrate, has no release tag as an ancestor, so a merge-base with it cannot serve. The sync workflow computes it on every run, weekly ones included, and fails with the reason when no upstream tag is reachable; until 2026-10-08 it counted stable tags only, so every weekly run from 2026-09-28 on failed at this step while the line pinned v0.2.0-beta5, and the mirror stayed where the last run had left it (#274, #276). `.circleci/config.yml` states it once, as the annotation `io.giantswarm.upstream.version` every image carries. |

## Carried patches

Everything on `giantswarm` that is not in the pin (`git log v0.5.0-alpha2..giantswarm`) has a ledger file of its
own in [`fork/`](fork/): one small file per change, so two concurrent pull requests never edit the same lines.
The directory is the index (`ls fork/`); this file carries no per-change rows. A file reads:

```markdown
---
id: <the file name without .md>
fork: substrate
theme: <one of the shared theme slugs>
category: pin | infra | bugfix | feature | gs-specific | compat | test
status: carried | dropped | superseded
commits:
  - '<the subject of each carried commit, as git shows it, without its (#N) suffix>'
upstream:
  outcome: merged | open | declined | superseded | to-file | fork-only | drops-at-repin | none
  refs: [owner/repo#N, ...]
  exit: <what makes the change leave the fork>
depends_on: [<a file of this ledger>, owner/repo:<a file of another line>, owner/repo#N]
verified: <YYYY-MM-DD, the day the upstream state was last checked>
---
# <short title>

## Change

What the commit does.

## Reason

Why the line carries it.

## History

What upstream and the line did about it, by date; prose, not state.
```

The front-matter is the single source of a change's state; nothing below it repeats it. A generator reads it, the prose is for people. `commits` names every commit the change carries by its subject, the one thing a re-pin keeps while it rewrites every SHA; `theme` is one of the shared slugs (`session-lifecycle` to `never-filed`); `upstream.refs` cite every upstream and fork item as `owner/repo#N`; `upstream.exit` says what makes the change leave the fork; `depends_on` names ledger files of this or another line (`owner/repo:<slug>` for another line) or an `owner/repo#N` item; `verified` is the day the upstream state was last checked. `## History` holds what the earlier bullets said, verbatim. `.github/scripts/ledger-header.sh substrate fork --complete origin/main` checks every header, that no subject is named twice, and, from the merge base with the mirrored upstream `main`, that every carried commit is named by exactly one file unless a prefix of `fork/EXEMPT` covers it (the ledger's own edits, the pins and markers the re-pin tooling writes, the bots); it runs in the `fork-ledger` workflow of every pull request, the same rules as `fork-ledger validate --complete`. `fork-ledger header` converts a file and seeds its header. Proposal and vocabulary: giantswarm/kagent-upstream#719.

A change adds or edits only its own file, in its own pull request; the file's name is the change's key. A re-pin
that makes a change redundant deletes its file (the paragraphs below name what each re-pin dropped). The
fork-infrastructure file (CI, publishing, sync, the ledger check) covers the changes to files that are not
Substrate's own.

Twenty-four patches change Substrate ahead of upstream — egress for an actor while it resumes, without which no
skill-carrying agent of the platform boots, the atelet scheduling knobs, the keep policy on the CRD chart's
templates, the kept podcertificate-controller namespace, without which a reinstall on the same cluster has
no working trust chain, the terminal classification of an image the registry refuses, without which a
misconfigured Harness image is never reported, the re-registration of a worker whose pod IP changed, the
pinned image set with its chart keys, and the lifecycle budgets (a workflow that outlives its caller, the
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
every template created on 1.0.x refuses its actors as not resumable after the upgrade, and the egress-policy contract check, without which a `kubectl-ate` of another line stores a policy with another meaning, and the template delete that takes the row first, without which a refused delete leaves a template whose golden is gone, and the fencing token on Pause, Suspend and Resume, without which a lease holder frozen past its lease pauses an actor its successor left running, and the lease-free durable-pause commit with the holder-naming lease, without which a client's pause or suspend right after a resume is Aborted by background work on a loaded database, and the denial's own gRPC code for a denied gRPC call, without which a gRPC caller of a held actor reads Unavailable and cannot tell it from an unavailable control plane, and the re-take of a golden written in an older snapshot layout, without which every new conversation on every agent fails after a release that changes the layout until the goldens are reset by hand; all twenty-four are written for upstream and leave at the first release that carries them. Two more are the line's own and stay: the read of a 1.8 actor's local-snapshot node and assigned node, without which a paused 1.8 actor cannot resume on v0.5.0-alpha2 and a running one crashes on its first pause, and the chart's own shape on the v0.5.0-alpha2 base, without which an upgrade loses the bundled database and the certificate controller. The image-identity patches are upstream's open
agent-substrate/substrate#1918 with the merged #1906 (2026-10-16) and #1910. The Postgres Secret patch is upstream's own pull
request, merged 2026-10-08 and in the v0.5.0-alpha2 pin, beside which the line keeps its own value key. Everything else is what upstream has already merged; the `x/net` v0.61.0 requirement changes versions only and leaves when upstream requires it (see its row). Giant Swarm specific wiring
lives elsewhere: the CA/JWT pool bootstrap (`kubectl-ate admin make-ca-pool`/`make-jwt-pool`
and the `ate-api-authentication` ConfigMap) is created by [agentlab](https://github.com/giantswarm/agentlab)
and by meta chart 4.0; the `WorkerPool` the platform's Harnesses run on comes with the kagent chart
(`kagent.substrateWorkerPool`); feature gates, Kyverno exceptions and network policies are cluster
configuration.

Dropped by [#455](https://github.com/giantswarm/substrate/pull/455), between re-pins, because the deviation moved out of
Substrate: the Google access-token provider (`981f2d4f`, #250, `google-access-token.k8s.io` as a second name of the bundled Kubernetes
credential provider) is a plugin of its own in upstream's shape, [giantswarm/substrate-credential-providers](https://github.com/giantswarm/substrate-credential-providers)
(`google-access-token`), routed through `credentialProvider.additionalProviders`; the bundled provider serves `k8s.io` alone again, as
upstream's does, and `ate-setup` renders one provider name. An installation keeps the plugin deployed and routed before it takes the
release that drops the name (agent-platform routes it with the floor bump); kagent-dev/substrate#47 closed unreviewed on 2026-10-12. The
routing value itself is still carried; its exit upstream is kagent-dev/substrate#90 (the chart's `credentialProvider.additionalProviders`,
nothing cloud-specific); `ate-setup` and the kustomize manifests upstream still render one provider.

Dropped at the 2026-10-12 re-pin onto v0.5.0-alpha2 because upstream carries them: the cherry-picks of agent-substrate/substrate#2110
(one rootfs-upper tar per container, `893b1edb`) and #2111 (device nodes and xattrs on extract, `8da76493`), the atelet refusal of a
trust-bundle projection with no names (upstream `601c03ba`, #2049), the identity e2e suite's egress trust-bundle rotation (upstream's
`AddEgressCA`), and the `serverboot` half of the `OTEL_TRACES_EXPORTER=none` patch (#2193). The 71 `docs(fork)` ledger commits of the
v0.4.0-alpha1 line are folded into the re-pin's ledger commit. Adapted in the replay, same behaviour: every carried
`status.Errorf`/`status.Code` on a control-API error is `apierror` (a gRPC status passed through `apierror.FromError` reads `Internal`,
so a carried `Unavailable` or `FailedPrecondition` would have reached the caller as `Internal`), in the crash teardown, `DeleteActor`'s
refusal, the transient-crash recovery, the golden re-take and the actor-transition reconciler; the crash teardown terminates on
`assigned_node`, so its tests seed the node and the recovery of a transient crash clears it for a SUSPENDED actor and points it at the
local snapshot's node for a PAUSED one; the durable pause records the snapshot's node in `LocalSnapshot.node_name` (field 5) beside
upstream's `assigned_node`, which a resume elsewhere moves; atelet's `ExternalVolumeSource.access_mode` and `read_only` are fields 10001
and 10002 (upstream's `publish_context` is field 4), carried on upstream's `MountVolumeRequest`; the fencing token is listed as no secret for the
redaction check; generated code regenerated with `hack/update/codegen.sh`.

Pull requests merged into `giantswarm` whose change the line no longer carries as a patch of its own (the line check names every merged
pull request): [#260](https://github.com/giantswarm/substrate/pull/260) (upstream's PATH_MAX-safe snapshot tars, the #2110 and #2111 cherry-picks dropped at the 2026-10-12 re-pin),
[#65](https://github.com/giantswarm/substrate/pull/65) (the identity e2e suite's egress trust-bundle rotation, dropped at the same re-pin), [#128](https://github.com/giantswarm/substrate/pull/128) (agent-substrate/substrate#1833,
the connection string no longer logged, dropped when upstream carried it), [#5](https://github.com/giantswarm/substrate/pull/5) and [#70](https://github.com/giantswarm/substrate/pull/70) (the agentgateway line's builds
`v1.5.1-gs.1` and 2.1.2 for the atenet data plane) and [#111](https://github.com/giantswarm/substrate/pull/111) with [#113](https://github.com/giantswarm/substrate/pull/113) (2.2.2, with the `agentgateway-image` parameter): the
pin table names the agentgateway the line runs now.

Dropped at the re-pin onto v0.0.29 (2026-09-14): the `cherry-pick -x` of kagent-dev/substrate#33 (atelet's
cluster-wide read of sandbox configs, `74b45f9e` → upstream `af7d1539`, in v0.0.29, which closes the row that carried it) and the chart half of #9 (the egress actor check as a frontend policy, which kagent-dev/substrate#28
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

## Upstream items prepared without a carried patch

Items the platform needs from Substrate whose fix is not yet written. The text below is the upstream-facing
proposal (it becomes an agent-substrate/substrate issue, a kagent-dev/substrate pull request when the patch
exists, or a kagent-dev/kagent issue); a human decides whether and when it goes upstream.

### Place an actor's restore only on a worker whose CPU covers the snapshot's feature set

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
   and the `Upstream` field of every ledger file the new pin changes.
4. Move the consumers to the new dev version (see "Consumers"), prove it in agentlab, then let the meta chart's
   pin and kagent-upstream follow.

The 2026-09-22 re-pin (v0.0.29 → v0.2.0-beta4) by hand, in a scratch clone: `git rebase --onto v0.2.0-beta4 v0.0.29`
replayed the carried patches (the runsc-terminate patch dropped, above), with hand resolutions in the egress
gateway, the worker syncer, ateapi's `main.go` and functional tests, `workflow_suspend.go` and the template reconciler
(upstream's constructor, listers, URI model and golden tags moved under the patches), and a regeneration of the generated
code; the chart image patches conflicted only on the `images.agentgateway` line, resolved to the patch's own value each
time. `go build ./... && go vet && go test ./cmd/... ./internal/...` and `helm unittest` locally, the suites in CI on the
`sync/20260922-v0.2.0-beta4` push; the force-push by a repository admin. The dev builds of the re-pinned branch start
from `0.0.0` again until the next release tag (the line's stable tags point at the pre-rebase history, which gitsemver
no longer reaches), so a consumer pins the full dev version.

The 2026-09-24 re-pin (v0.2.0-beta4 → v0.2.0-beta5) by hand, in a scratch worktree: `git rebase --onto v0.2.0-beta5
v0.2.0-beta4` replayed every carried patch (nothing dropped, above), with hand resolutions in the chart README rows,
the functional tests' placement and fake atelet, the generated protobuf code and the kubectl-ate admin file upstream
consolidated. `go build ./... && go vet ./... && go test ./cmd/... ./internal/...`, `helm unittest charts/substrate` and
`hack/verify-all.sh` locally (`internal/childreap`'s p90 timing test failed once under a loaded workstation and passed
again on the same tree); the suites in CI on the `sync/20260924-v0.2.0-beta5` push; the force-push by a repository admin — who first lifts the admin bypass of the
repository manager's `devctl: default branch` ruleset (present since 2026-09-24; it admits admins for pull requests only
and refuses a force-push otherwise) to `always` and restores it right after.
The dev builds start from `0.0.0` again, so the consumers pin the full dev version.

The 2026-10-05 re-pin (v0.2.0-beta5 → v0.3.0-alpha3) by hand, in a scratch worktree, from the 2026-09-29 probe candidate
(`sync/20260929-v0.3.0-alpha1`, the carried stack already replayed onto v0.3.0-alpha1): `git rebase --onto v0.3.0-alpha3 v0.3.0-alpha1`
of its commits, then the commits `giantswarm` gained since 2026-09-29 cherry-picked in order; the hand resolutions and the one
drop are listed under "Carried patches". The `docs(fork)` ledger commits of the beta5 line (every FORK.md edit since the bootstrap, the release-table rows
among them) are folded into this ledger commit: the replayed stack is the code patches and the fork's own CI, publish and
alignment changes. The pin annotation moved to v0.3.0-alpha3 (`0cfdba6c`) and the data plane to the agentgateway
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
of the commits of `giantswarm` at `47c6868e`. One applied upstream on its own (`git cherry`); these are dropped because upstream carries
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

The 2026-10-12 re-pin (v0.4.0-alpha1 → v0.5.0-alpha2) by hand, in a scratch worktree: `git rebase --onto v0.5.0-alpha2 v0.4.0-alpha1`
of the commits of `giantswarm` at `fe803596`, the ledger-only commits dropped and folded into this ledger commit; the drops and
hand resolutions are listed under "Carried patches". The chart is rebuilt by a three-way merge of the line's chart with upstream's
v0.4.0-alpha1 → v0.5.0-alpha2 chart change, every PostgreSQL and certificate-controller conflict to the line's side (see its ledger file).
`go build ./... && go vet ./... && go test ./...` and `helm unittest charts/substrate` (100/100) locally; the suites in CI on the
`sync/20261012-v0.5.0-alpha2` push. The pin annotation moves to v0.5.0-alpha2 in `.circleci/config.yml`; the egress-policy contract stays `v0.4.0-alpha1`
(`EgressPolicy` and `EgressRule` are unchanged); the data plane stays on the agentgateway line's 2.3.0.

These entries carry no commit counts: the rebase rewrites the history they would count, and the ledger rows of `fork/` and
"Carried patches" at the commit the entry names are the record of what was carried and dropped.

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
CircleCI's `architect` context only, and the signing identity is the pipeline's own. `fork-ledger` fails a pull
request or a re-pin candidate whose tree has a workflow file naming `ghcr.io`, so a publishing workflow upstream adds
is removed before it lands.

| Artifact | Name |
|---|---|
| Control plane and node images | `gsoci.azurecr.io/giantswarm/substrate/{ateapi,atecontroller,atelet,atenet,podcertcontroller,kubernetes-secrets}:<version>` (the last is `cmd/credential-provider/kubernetes-secrets`, the chart's `k8s-credential-provider` the egress gateway injects Secret-backed credentials from; `COMPONENT_PATH` names its source directory) — linux/amd64 + linux/arm64, `.circleci/Dockerfile` with `COMPONENT=<name>`: `./cmd/<name>` compiled as ko compiles it (`CGO_ENABLED=0`, `-trimpath`, the vendored modules, the VCS revision embedded) at `/ko-app/<name>` on the distroless base `.ko.yaml` pins, `/ko-app` on `PATH`, the binary the ENTRYPOINT; the index signed, with provenance and SBOM |
| Worker image | `gsoci.azurecr.io/giantswarm/substrate/ateom-gvisor:<version>` — the `WorkerPool.spec.workerImage` of the platform's pool, built the same way; the index signed |
| agentgateway | not built or mirrored here: `images.agentgateway` is stamped to a **release of the Giant Swarm line of agentgateway**, published by that line from CircleCI under its own gsoci path — `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:<X.Y.Z>` (how it gets there is that line's `FORK.md`), the `agentgateway-image` parameter of `.circleci/config.yml` — which atenet-router and atenet-egress run ([giantswarm/agentgateway-upstream `FORK.md`](https://github.com/giantswarm/agentgateway-upstream/blob/giantswarm/FORK.md)) |
| Charts | `oci://gsoci.azurecr.io/giantswarm/substrate/helm/substrate-crds:<version>`, `oci://gsoci.azurecr.io/giantswarm/substrate/helm/substrate:<version>` — `image.registry`, `image.repository` and `image.tag` stamped to this registry, `images.agentgateway` to the agentgateway line's release; `version` = `appVersion` = the image tag; each chart signed |

Not published from here: the `substrate-podcert` chart, which the tree carries from upstream and no consumer of the line
installs: the line's `substrate` chart installs the pod certificate controller itself, so the two charts render the same
controller (its Deployment, Role and RoleBinding in `podcertificate-controller-system` and the `podcert-ate-dev-signer`
ClusterRole and ClusterRoleBinding), and whichever release installs second fails on Helm's ownership check. A consumer
of upstream kagent's `kagent install` skips its `substrate-podcert` step on this line. The known gap: the line's chart
runs the `servicedns` and `podidentity` signers only, and `postgres.podcert.ate.dev`, the signer upstream kagent's
bundled PostgreSQL authenticates its clients with, exists only in `substrate-podcert`; a feature that relies on
podcert-authenticated PostgreSQL needs that signer in the line's `substrate` chart first. `ateom-microvm` and the demo images
(the platform runs gVisor workers only),
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
- One commit releases one version: a candidate and its stable share a commit, two versions never do. A release tag
  whose commit already carries the release tag of another version publishes nothing: the pipeline's first job,
  `release-tag` (`.github/scripts/release-tag.sh`), fails naming both tags before any push, and its fixture runs
  (`release-tag-test.sh`) run on every pull request branch. Such a change is released from a commit of its own.

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
| **v0.0.30-gs.1** (2026-09-14, tag on `21027aa1` = the re-pin onto kagent-dev/substrate **v0.0.29** with the ten carried patches replayed, the agentgateway line's `v1.5.1-gs.4` pinned for the router and egress gateway, the suites on `sync/**` pushes; the first release of the new base — cut after the agentlab proof of the three lines together on the same tree's dev build `0.0.30-dev.giantswarm.2026-09-14.19-48-17.h21027aa`: `platform-test` 7/7, `agents-test`, three `skills-test` fixtures (public, `frontmatter-fields`, private with `credentialRef`), `a2a-test` 5/5, a unary A2A turn outliving its caller's deadline, `toolsets-test` 11/11, as an upgrade from `0.0.27-gs.9`) | v0.0.29 | images `ateapi` `sha256:79816517…`, `atecontroller` `sha256:baaffc43…`, `atelet` `sha256:a254fdad…`, `atenet` `sha256:aa0a7f7c…`, `podcertcontroller` `sha256:02ccb3e3…`, `ateom-gvisor` `sha256:cab2bb94…` (linux/amd64 + arm64); charts `substrate` `sha256:abb39761…`, `substrate-crds` `sha256:8beaea23…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34895330411](https://github.com/giantswarm/substrate/actions/runs/34895330411), every scan clean. Consumers: kagent-upstream `Makefile` `SUBSTRATE_VERSION` (giantswarm/kagent-upstream#37 → `v0.11.0-gs.14`), the agent-platform meta chart's ranges `>=0.0.30-gs.1 <0.0.31-0` |
| **v0.0.30-gs.2** (2026-09-15, annotated tag on `41c1a66a` = gs.1 + #35: a paused actor's resume fails fast when its node is gone and crashes it when no durable snapshot remains, `LOCAL_SNAPSHOT_GONE` (`860f257d`; its ledger row follows from that pull request's session) + #33: `WorkerPool.spec.template` takes `topologySpreadConstraints` and `podAntiAffinity` (`6f2ef0d2`, with its ledger row `41c1a66a`); cut after the agentlab proof of #33's dev build `0.0.30-dev.fork-workerpool-spread.2026-09-15.10-40-00.hd810518`: the spread stored unpruned on the WorkerPool and on the Deployment's pod template, owned by `workerpool-controller` under server-side apply, `platform-test` 9/9 and `agents-test` green) | v0.0.29 | images `ateapi` `sha256:8e066d15…`, `atecontroller` `sha256:3d1d4e27…`, `atelet` `sha256:6dfee809…`, `atenet` `sha256:cf5002a9…`, `podcertcontroller` `sha256:f2335b9d…`, `ateom-gvisor` `sha256:9ffc464c…` (linux/amd64 + arm64); charts `substrate` `sha256:1d2c0d7b…`, `substrate-crds` `sha256:fb336334…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34978676815](https://github.com/giantswarm/substrate/actions/runs/34978676815), every scan clean. Consumers: the agent-platform meta chart's `workerPoolSpreadFloor` and Substrate range floor (giantswarm/agent-platform#472); the kagent line stamps it at its next re-pin — the worker image is functionally gs.1's (the release changes ate-controller, the CRD and ate-api only) |
| **v0.0.30-gs.3** (2026-09-15, tag on `653de64e` = gs.2 + #34: every pause durable — the pause snapshot uploaded in the background and recorded as the actor's external snapshot marked with its source, the snapshot's node a placement preference once the copy exists, a suspend adopting the copy without the node (`653de64e`)) | v0.0.29 | images `ateapi` `sha256:4bb59829…`, `atecontroller` `sha256:9de1965d…`, `atelet` `sha256:8079258f…`, `atenet` `sha256:515de099…`, `podcertcontroller` `sha256:19e628ae…`, `ateom-gvisor` `sha256:4ad380f2…` (linux/amd64 + arm64); charts `substrate` `sha256:c08d9122…`, `substrate-crds` `sha256:71cf502e…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 34981525932](https://github.com/giantswarm/substrate/actions/runs/34981525932). Consumers: the agent-platform meta chart's range `>=0.0.30-gs.1 <0.0.31-0` admits it without a re-pin — gazelle rolled it at the next 10-minute reconcile of its OCIRepository (chart pushed 14:28Z, HelmRelease Ready on gs.3 with every pod and all 16 atelets updated at 14:40Z); its one `PAUSED` actor is the incident's dead session, which the resync reports every minute as `No atelet on the node holding the pause snapshot; not uploaded` until #35 crashes it |
| **v0.0.30-gs.4** (2026-09-15, annotated tag on `d8a2d5e4` = gs.3 + #39: the golden boot a workload keeps failing is bounded — three boots counted in `GoldenSnapshotStatus.workload_boot_failures`, then the golden actor crashed (its worker freed) and the template failed with `GoldenActorNotReady`, the bound and the last boot's error — and ateom's readiness-deadline error quotes the workload's last output lines (`7a11e479`, with its ledger row `d8a2d5e4`); cut after the agentlab proof of the branch's dev build `0.0.30-dev.fork-golden-boot-bound.2026-09-15.18-17-09.he684b13`: the released images loop a private-skill template without end, the patch fails it 3 min 8 s after creation with `could not read Username for 'https://github.com'` in kagent's condition message, the spec fix reaches Ready in 20 s and the failed revision is collected; `platform-test` green) | v0.0.29 | images `ateapi` `sha256:1703b848…`, `atecontroller` `sha256:4ae8e4ac…`, `atelet` `sha256:d36093c3…`, `atenet` `sha256:ddb41ec3…`, `podcertcontroller` `sha256:0fc2e4b6…`, `ateom-gvisor` `sha256:6e086b8c…` (linux/amd64 + arm64); charts `substrate` `sha256:5dfa7ae1…`, `substrate-crds` `sha256:bc5b90aa…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.4`; [run 35009210132](https://github.com/giantswarm/substrate/actions/runs/35009210132), every scan clean. Consumers: the agent-platform meta chart's range `>=0.0.30-gs.2 <0.0.31-0` admits it without a re-pin (the control plane's bound); the worker image — the quoted output — comes with the kagent line's stamp (giantswarm/kagent-upstream#49, `SUBSTRATE_VERSION 0.0.30-gs.1 → 0.0.30-gs.4`) through the kagent range `>=0.11.0-gs.16 <0.11.1-0` |
| **v0.0.30-gs.5** (2026-09-19, annotated tag on `ea00cfce` = gs.4 + #42/#43: every Docker Hub image default names its registry (`images.awsCli` kept a short name for the immutable bucket-init Job) + #45: the third-party image defaults are their gsoci copies, `images.agentgateway` the agentgateway line's release on gsoci + #46: the line publishes its images and charts to gsoci from CircleCI — **the first release published natively to `gsoci.azurecr.io/giantswarm/substrate`, each index and chart signed with the organisation's CircleCI identity**; cut after the agentlab proof of #46's dev build `0.0.30-dev.fork-circleci-gsoci.2026-09-19.04-30-17.hab9aa1f` from gsoci: the control plane, the atelet and the four workers on the new images, `platform-test` 13/13, `agents-test` 6/6 with the golden boot Ready in 12 s) | v0.0.29 | images `ateapi` `sha256:6692117e…`, `atecontroller` `sha256:27f95c3e…`, `atelet` `sha256:8ad7a4ec…`, `atenet` `sha256:da4e699d…`, `podcertcontroller` `sha256:6a314e07…`, `ateom-gvisor` `sha256:cf816037…` (linux/amd64 + arm64); charts `substrate` `sha256:943f8a95…`, `substrate-crds` `sha256:ebe2b126…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway:v1.5.1-gs.4` (`sha256:f3d4b52c…`); [CircleCI pipeline 6](https://app.circleci.com/pipelines/github/giantswarm/substrate/6) (`images` 1 min 57 s, `scan` 31 s, `charts` 17 s), every scan clean, every reference `cosign verify`-ed under the pipeline's identity. Consumers: the agent-platform meta chart's chart sources move to the gsoci path (giantswarm/agent-platform#580) — until then no installation follows this release, its chart range still reads the ghcr path; the kagent line's `SUBSTRATE_REPO`/`SUBSTRATE_VERSION` move together at its re-pin; agentlab's defaults (giantswarm/agentlab#229); retagger's `renamed-substrate.yaml` mirror entries are retired now that a native release exists (giantswarm/retagger#1238, open) |
| **1.0.0** (2026-09-19, annotated tag on `171b9e44` = gs.5 + #48: the six images built by the architect orb's `push-to-registries` jobs from `.circleci/Dockerfile` (ko's result; each index annotated `io.giantswarm.upstream.version=v0.0.29`, with provenance and an SPDX SBOM), the versions decoupled from upstream's — **the first release of the scheme**, the next major above `0.0.30-gs.5` — and `images.agentgateway` the agentgateway line's own release `agentgateway-upstream/agentgateway:2.0.0`) | v0.0.29 | images `ateapi` `sha256:d99a5316…`, `atecontroller` `sha256:bdb0b08f…`, `atelet` `sha256:0c647af8…`, `atenet` `sha256:44335714…`, `podcertcontroller` `sha256:9df643a6…`, `ateom-gvisor` `sha256:1ea67204…` (linux/amd64 + arm64; each index and its per-platform SPDX attestation signed); charts `substrate` `sha256:291e1b41…`, `substrate-crds` `sha256:99d8211e…` (signed); dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.0.0` (`sha256:63deaa67…`); [CircleCI pipeline 13](https://app.circleci.com/pipelines/github/giantswarm/substrate/13) (pushes 1 min 55 s – 4 min 29 s, `scan` 22 s, `charts` 24 s), every scan clean, every reference `cosign verify`-ed under the pipeline's identity; the orb also moves the images' `latest` tag to the release. The dev build of the same commit is `0.0.0-dev.giantswarm.2026-09-19.13-38-12.h171b9e4` (pipeline 12). Consumers: the agent-platform meta chart's ranges (giantswarm/agent-platform#580), the kagent line's `SUBSTRATE_VERSION` at its re-pin, agentlab's defaults (giantswarm/agentlab#229), retagger's `renamed-substrate.yaml` entries retired (giantswarm/retagger#1238) |
| **1.0.1** (2026-09-22, annotated tag on `2781d80d` = 1.0.0 + #50: PodMonitors for the six workloads that serve metrics (`metrics.podMonitor`, off by default; one object per workload, each pinned to the port name its own pod declares — `prometheus`, `metrics`, `gw-metrics`, `stats` — and the credential provider's to the `substrate.fullname` pod label), with the `metrics.podMonitor.labels` knob the platform sets to the tenant label. Upstream: kagent-dev/substrate#46, open — carried until it merges and the next re-pin drops it. Chart-only and inert until a consumer enables it, so the release is a patch and the agent-platform range `>=1.0.0 <1.1.0` admits it without a re-pin) | v0.0.29 | images `ateapi` `sha256:79296cd2…`, `atecontroller` `sha256:d6c4897e…`, `atelet` `sha256:84936bf9…`, `atenet` `sha256:54ea7ae3…`, `podcertcontroller` `sha256:6d17ba53…`, `ateom-gvisor` `sha256:5f037d42…` (linux/amd64 + arm64); charts `substrate` `sha256:e78db7fe…`, `substrate-crds` `sha256:00554ec9…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.0.0` |
| **1.0.2** (2026-09-23, annotated tag on `a7787e36` on `release-1.0` = 1.0.1 + #60: the agentgateway line's **2.1.1** for atenet-router and atenet-egress (`agentgateway-image` in `.circleci/config.yml` and the chart default `images.agentgateway`): a managed file resource is re-read on every fetch, so the `backendTLS` client certificate of `substrateEgressActorResolution` follows kubelet's PodCertificate rotation instead of expiring in place 24 h after pod start (giantswarm/agentgateway-upstream#20; the 2026-09-23 outage) + the cherry-pick of #58's `podCertificates.maxExpirationSeconds` (`aed0b8d1`; nine sources on this tree, no credential provider). The 1.0.1 egress and router configs validate unchanged against 2.1.1 (`--validate-only`). Cut for the fleet's 1.0.x installations because `giantswarm` is on the beta4 contract; the agent-platform meta chart moves its floor to it together with `substrate.images.agentgateway` (giantswarm/agent-platform#648)) | v0.0.29 | images `ateapi` `sha256:c9edda7f…`, `atecontroller` `sha256:efada927…`, `atelet` `sha256:6f68c35f…`, `atenet` `sha256:a7132403…`, `podcertcontroller` `sha256:4f8f160e…`, `ateom-gvisor` `sha256:f141945d…` (linux/amd64 + arm64); charts `substrate` `sha256:92aa5d9f…`, `substrate-crds` `sha256:bf694053…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.1` (index `sha256:825c2f9e…`); [CircleCI pipeline 52](https://app.circleci.com/pipelines/github/giantswarm/substrate/52), every scan clean |
| **1.0.3** (2026-09-23, annotated tag on `29fd1a12` on `release-1.0` = 1.0.2 + #63: the agentgateway line's **2.1.2** for atenet-router and atenet-egress — the certificate files a Substrate policy's `policies.backendTLS` names, read at parse time and never managed before, become managed dependencies (watched, active, re-read every 60 s), so the client certificate the router's `substrateIngress` and the egress gateway's `substrateEgressActorResolution` present to ate-api follows kubelet's PodCertificate rotation whichever of the pod's two bundles is written first; 2.1.1 followed it only when the podidentity bundle was on disk before the servicedns event's reload (giantswarm/agentgateway-upstream#23). Both 1.0.x gateway configs validate unchanged against 2.1.2 (`--validate-only`); the data plane is the only rendered change. Consumer: the agent-platform meta chart's floor and `substrate.images.agentgateway` (giantswarm/agent-platform#653)) | v0.0.29 | images `ateapi` `sha256:a4c2895e…`, `atecontroller` `sha256:b3f93811…`, `atelet` `sha256:2fce19af…`, `atenet` `sha256:694c788f…`, `podcertcontroller` `sha256:33928dc7…`, `ateom-gvisor` `sha256:61e79663…` (linux/amd64 + arm64); charts `substrate` `sha256:e78f5f2d…`, `substrate-crds` `sha256:f6119ca9…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.2` (index `sha256:a29ec879…`); [CircleCI pipeline 62](https://app.circleci.com/pipelines/github/giantswarm/substrate/62), every scan clean |
| **1.0.4** (2026-09-24, annotated tag on `d60beed9` on `release-1.0` = 1.0.3 + #77: the bundled RustFS Deployment recreates its pod instead of rolling it — one replica on the ReadWriteOnce claim `rustfs-data`, so a rolling update's new pod could not attach the volume the old pod held and the release waiting on it stalled. The 1.1 line fixes the same stall with `RollingUpdate` `maxSurge: 0`/`maxUnavailable: 1` (#83), since `Recreate` is refused by server-side apply over a defaulted rolling strategy on an in-place upgrade. The agent-platform meta chart's 1.0 range `>=1.0.0 <1.1.0` admits it; no consumer follows that range any more) | v0.0.29 | images `ateapi` `sha256:29f18a41…`, `atecontroller` `sha256:dbb013cc…`, `atelet` `sha256:d38133b6…`, `atenet` `sha256:898c3bb1…`, `podcertcontroller` `sha256:c12c1b60…`, `ateom-gvisor` `sha256:481a14cd…` (linux/amd64 + arm64); charts `substrate` `sha256:92c82e25…`, `substrate-crds` `sha256:46df18a7…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.1.2` (index `sha256:a29ec879…`); [CircleCI pipeline 133](https://app.circleci.com/pipelines/github/giantswarm/substrate/133) |
| **1.1.0** (2026-09-24, tag on `7fd77115` = the 2026-09-24 re-pin of `giantswarm` onto **v0.2.0-beta5** with every carried patch replayed (#73's ledger, nothing dropped; upstream `main` was rewritten onto agent-substrate, so beta5 is not a descendant of beta4 and every commit replayed) + #56: `ActorTemplate.default_egress_policy`, copied onto every actor of the template as its `default` policy, so a golden boot that fetches skills is admitted by atenet (the kagent line sets it from the revision's egress destinations, kagent-upstream#88) + #74: the agentgateway line's **2.2.0** for atenet-router and atenet-egress. **The first release of the beta5 contract**: a minor above 1.0.3 because kagent's ate-api client moves with it (kagent-upstream 1.1.0 stamps this release in `SUBSTRATE_VERSION` and its `go.mod` replace); `release-1.0` stays for the v0.0.29 contract. Cut after the agentlab proof of the dev builds `0.0.0-r588f3d76t20260924131333h6c6d012` (Substrate) and `0.9.11-r588f3d76t20260924141400h1f36e2c` (kagent): `platform-test` 11/11, `a2a-test` 5/5, `skills-test` public 3/3 and private 3/3 with the wrong-token control failing, `agents-test` 6/6 (giantswarm/agent-platform#665)) | v0.2.0-beta5 | images `ateapi` `sha256:07486cc1…`, `atecontroller` `sha256:9260fc1d…`, `atelet` `sha256:293ef348…`, `atenet` `sha256:800c69ee…`, `podcertcontroller` `sha256:b511598f…`, `ateom-gvisor` `sha256:5def3575…` (linux/amd64 + arm64); charts `substrate` `sha256:b110b36a…`, `substrate-crds` `sha256:0c1c999c…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.0` (index `sha256:370827be…`); [CircleCI pipeline 110](https://app.circleci.com/pipelines/github/giantswarm/substrate/110), every scan clean. Consumer: the agent-platform meta chart's ranges `>=1.1.0 <1.2.0` for `substrate`/`substrate-crds` together with kagent 1.1.0 (giantswarm/agent-platform#665) |
| **1.1.1** (2026-09-25, annotated tag on `18e2f1a0` = 1.1.0 + #80: a resume whose golden snapshot is gone is refused as `FailedPrecondition` with the ErrorInfo reason `GOLDEN_SNAPSHOT_UNAVAILABLE` and the metadata `resumable: "false"`, so a conversation started before a runtime upgrade ends at once instead of timing out (giantswarm/substrate#79) + #76 and #83: the bundled RustFS Deployment rolls with `maxSurge: 0` and `maxUnavailable: 1`, its old pod releasing the ReadWriteOnce claim before the new one starts (#83 supersedes #76's `Recreate`, which server-side apply refuses on an in-place upgrade) + #81: the agentgateway line's **2.2.1** for atenet-router and atenet-egress, whose ingress answers a `resumable: false` refusal with the control plane's status at once (giantswarm/agentgateway-upstream#29) + #82: serverboot honours `OTEL_METRICS_EXPORTER=none`. Checked before the tag by a server-side dry-run of the rendered chart as `helm-controller` against a live 1.1.0 release. The agent-platform meta chart's ranges `>=1.1.0 <1.2.0` admit it without a re-pin; the worker image stays the kagent line's stamp) | v0.2.0-beta5 | images `ateapi` `sha256:5e911cd5…`, `atecontroller` `sha256:c6435fa5…`, `atelet` `sha256:0163ef4b…`, `atenet` `sha256:80eac231…`, `podcertcontroller` `sha256:385096b2…`, `ateom-gvisor` `sha256:574c9106…`, `kubernetes-secrets` `sha256:6da901b4…` (linux/amd64 + arm64); charts `substrate` `sha256:c58ae6fe…`, `substrate-crds` `sha256:0b5aaffb…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` (index `sha256:b3decd06…`); [CircleCI pipeline 144](https://app.circleci.com/pipelines/github/giantswarm/substrate/144) |
| **1.1.2** (2026-09-27, annotated tag on `74036f9a` = 1.1.1 + #85: the `rustfs-bucket-init` Job is a Helm hook (`post-install,post-upgrade`, `before-hook-creation,hook-succeeded`), recreated on every install and upgrade, and `images.awsCli` defaults to the gsoci copy `gsoci.azurecr.io/giantswarm/aws-cli:2.17.0`, so the Job's image can move without an upgrade failing on its immutable pod template (giantswarm/agent-platform#580). Proven before the merge on an agentlab through helm-controller, 1.1.1 → the patched chart (old Job deleted, hook Job from gsoci completed and removed) and back (plain Job recreated, no conflict); after the tag, an agentlab upgrade from agent-platform 4.86.0 to the meta chart that forwards the gsoci value (agent-platform#716, released in 4.87.0 with the ranges `>=1.1.2 <1.2.0`)) | v0.2.0-beta5 | images `ateapi` `sha256:5ec924a8…`, `atecontroller` `sha256:8731d518…`, `atelet` `sha256:a2a200e6…`, `atenet` `sha256:07823948…`, `podcertcontroller` `sha256:c63a3f80…`, `ateom-gvisor` `sha256:82c7ad5c…`, `kubernetes-secrets` `sha256:bed77a31…` (linux/amd64 + arm64); charts `substrate` `sha256:a3c2ff4d…`, `substrate-crds` `sha256:d83561a1…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.1.3** (2026-09-28, annotated tag on `b23604fa` = 1.1.2 + #87: `atelet.priorityClass.preemptionPolicy`, `PreemptLowerPriority` by default (unchanged) or `Never`, which renders the class `ate-node-critical-non-preempting` because a PriorityClass's `preemptionPolicy` is immutable; with `Never` atelet keeps its priority and evicts nothing (giantswarm/agent-platform#731). Proven before the tag on an agentlab through helm-controller on the dev build `1.1.3-r588f3d76t20260928113438hb23604f`: 1.1.2 → `Never` (the old class deleted, atelet rolled onto the new one); a node filled by a low-priority pod keeps it, atelet Pending with `preemption: not eligible due to preemptionPolicy=Never` and Ready once room appears, where 1.1.2 evicted the filler; its consumer is giantswarm/agent-platform#734 (the ranges `>=1.1.3 <1.2.0`, `Never`)) | v0.2.0-beta5 | images `ateapi` `sha256:727defec…`, `atecontroller` `sha256:b454f3ef…`, `atelet` `sha256:bceab5dc…`, `atenet` `sha256:6e22635f…`, `podcertcontroller` `sha256:01d45a46…`, `ateom-gvisor` `sha256:1bbc306f…`, `kubernetes-secrets` `sha256:0fc94b3b…` (linux/amd64 + arm64); charts `substrate` `sha256:359bd890…`, `substrate-crds` `sha256:4933664f…`; dataplane `gsoci.azurecr.io/giantswarm/agentgateway-upstream/agentgateway:2.2.1` |
| **1.1.4** (2026-09-29, tag on `1565be1b`, the commit 1.2.0 names: **1.2.0 is that commit's version**. The tag stays where it is, since its images and charts are published and a released tag is never moved or deleted; no later release lands on a commit that carries another version's tag ("Versions")) | v0.2.0-beta5 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.2.0** (2026-09-29, tag on `1565be1b`: the first minor of the 1.2 line and the canonical version of that commit, which 1.1.4 also names) | v0.2.0-beta5 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.3.0** (2026-09-30, tag on `2b76e00a`: `PodAffinity` on the worker pod template and the platform's aligned repository files; `release-1.3` follows as a maintenance branch) | v0.2.0-beta5 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.3.1 – 1.3.6** (2026-10-06 – 2026-10-07, tag on `577984a8`, `c7cae9e0`, `28d7fc02`, `9024e3eb`, `68409a86`, `d9c5be30`: release-1.3 backports as patches: atelet reads its pinned images from a file, the template row is deleted before its golden actor and tag, validation skips the tagged maintenance branches) | v0.2.0-beta5 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.3.7** (2026-10-08, tag on `42230a56`: Pause, Suspend and Resume fenced with a token; the raw PostgreSQL connection string is no longer logged at startup) | v0.2.0-beta5 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.4.0** (2026-10-05, tag on `e27ae953`: the re-pin onto v0.3.0-alpha3: `actor_template_uid` per snapshot, resolved sandbox assets sent during restore, the global image values, atelet scheduling knobs and preemption policy, PodMonitors and pod labels, extra CAs for the egress gateway) | v0.3.0-alpha3 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.5.0** (2026-10-08, tag on `314cbe4f`: the egress-policy contract report and the lease-token fence; OpenTelemetry semantic conventions and no export without an endpoint; orphan reaping while tracked subprocesses run; the OpenFGA schema adoption; golden statuses recorded by older releases migrated) | v0.3.0-alpha3 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.6.0** (2026-10-08, tag on `85d02ece`: the re-pin onto v0.4.0-alpha1: a credential grant narrowed by secret name or label and the spawn benchmark, on top of the 1.4 and 1.5 changes) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.6.1** (2026-10-09, tag on `315b9ff5`: `x/net` v0.60.0 on Go 1.27.2; the sandbox resolv.conf keeps only the cluster's search domains) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.6.2** (2026-10-09, tag on `07c1474c`: the egress ext-proc's telemetry named `atenet-egress`) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.6.3** (2026-10-09, tag on `e650362f`: the gVisor SandboxConfig's pause image is a chart value that defaults to its gsoci copy) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.7.0** (2026-10-10, tag on `90654619`: an existing volume mounted per actor at a sub-path, a missing read-write sub-path created at the bind, the actor's hostname named in its `/etc/hosts`; Docker Hub images pulled through mirror.gcr.io) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.0** (2026-10-11, tag on `eb73a7ff`: further credential providers for the egress gateway (Google access tokens minted from service-account key Secrets), its ephemeral storage bounded, a crashed actor's workload torn down before the actor is deleted, a restore from the pre-per-volume durable layout refused, `x/net` v0.61.0 for two HTTP/2 CVEs, device nodes and unvetted xattrs refused on tar extract) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.1** (2026-10-11, tag on `a73e9d36`: a pre-per-volume durable snapshot converted on restore, a golden written in an older snapshot layout taken again, the sync workflow names the reason when no upstream pin resolves) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.2** (2026-10-12, tag on `c786c015`: a golden take, a resume and a restore that a rolling worker pool or a worker drain interrupts are retried) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.3** (2026-10-12, tag on `f8850816`: a WorkerPool's worker image named in its status, the atelet DaemonSet rolling a quarter of the nodes at a time, a trust-bundle projection with no names refused, a resume a worker drain cuts answered `Unavailable`) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.4** (2026-10-12, tag on `aca5b418`: a golden take whose golden actor is in a stray state or was failed by the trust-bundle guard retried, a drain-cut checkpoint returned to the last durable snapshot) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.8.5** (2026-10-13, tag on `fe803596`: an actor the trust-bundle guard crashed resumes, a read-only volume mount published with the volume's own mode — the current stable of the 1.8 line) | v0.4.0-alpha1 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.9.0** (2026-10-12, tag on `0731c791`: the re-pin onto v0.5.0-alpha2 with the 1.8 line's changes carried: the atelet image credential provider config as text, a lease token fencing Pause, Suspend and Resume, egress refusals counted by actor template and reason) | v0.5.0-alpha2 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.9.1** (2026-10-13, tag on `4bede241`: the agentgateway line's 2.3.5 in the router and egress gateway, a 403 route outcome classified as denied, the large object-storage transfers a process runs at once bounded, the removed `SnapshotConfig.on_pause` accepted for a transition) | v0.5.0-alpha2 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **1.10.0** (2026-10-13, tag on `4d86c8b3`: the credential path prefix in the egress contract `v0.4.0-alpha1+path-prefix`, a running actor whose worker went away returned to its own durable snapshot, a resume never committing `RUNNING` on a worker that started leaving) | v0.5.0-alpha2 | images and charts as published by the release's CircleCI pipeline; the digests are those of the release's `image-refs.txt` and `chart-refs.txt` artifacts |
| **v0.0.27-gs.9** (2026-09-13, tag on `36d00f80` = gs.8 + #30: the gVisor worker's terminate collects a workload whose runsc containers are already gone (`1dd5d1fb` — `cleanupContainers` lists the containers runsc knows and checks and deletes only those; a delete that fails after removing its container counts as done), with its ledger row) | v0.0.26 | images `ateapi` `sha256:cb267526…`, `atecontroller` `sha256:f2c845ab…`, `atelet` `sha256:0882ecb1…`, `atenet` `sha256:72939cda…`, `podcertcontroller` `sha256:d3eab8cc…`, `ateom-gvisor` `sha256:2240d996…` (linux/amd64 + arm64); charts `substrate` `sha256:bb6ba693…`, `substrate-crds` `sha256:09e363c6…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34783226348](https://github.com/giantswarm/substrate/actions/runs/34783226348), every scan clean |
**Scans.** Every own image is scanned with Trivy (HIGH and CRITICAL, fixable only) after the push and before the
charts that reference it are published. A fixable finding fails the publish: bump the module (upstream first) or,
when upstream has no fix, add a time-boxed entry to `.trivyignore` (`CVE-… exp:YYYY-MM-DD # reason, tracking
issue`) — an expired entry fails again and is re-triaged, not extended. The agentgateway image is scanned
report-only here: it is the agentgateway line's build, its scan gates its own publish and a finding is fixed there. `govulncheck` covers the Go module graph on every push, pull
request and weekly.

## Consumers

Each consumer moves to the decoupled versions (`1.0.0` and up, "Versions") in its own change: the agent-platform
meta chart's ranges (giantswarm/agent-platform#580), the kagent line's `SUBSTRATE_VERSION` at its re-pin,
agentlab's defaults (giantswarm/agentlab#229), and retagger's
`renamed-substrate.yaml` entries, retired (giantswarm/retagger#1238).

| Consumer | Where the pin lives | Selects |
|---|---|---|
| [agentlab](https://github.com/giantswarm/agentlab) | `internal/lab/substrate.go` (`substrateChartsRepo`, `substrateImageRegistry`, `substrateVersion`) | an exact dev version or release; installs `substrate-crds` + `substrate` and preloads the worker image |
| agent-platform meta chart 4.0 (`components.substrate` / `components.substrate-crds`, the `substrate:` values block; [giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342)) | `helm/agent-platform/values.yaml`: the two components' version ranges and `kagent.substrateWorkerPool.workerImage` | the `substrate-crds` + `substrate` charts by release range (`>=1.0.0 <2.0.0`) or at an exact dev version, and the `ateom-gvisor` image at the same version (the `WorkerPool` the kagent chart renders) |
| [giantswarm/kagent-upstream](https://github.com/giantswarm/kagent-upstream) | `Makefile` `SUBSTRATE_REPO ?= oci://gsoci.azurecr.io/giantswarm/substrate/helm` (the kagent line follows the path with its own move to gsoci), `SUBSTRATE_VERSION` | the `substrate`/`substrate-crds` chart dependencies of the kagent charts (off in the platform, which installs Substrate as cluster infrastructure); its `kagent install` skips upstream's `substrate-podcert` step, since the line's `substrate` chart installs the certificate controller |

## Assets that are not images

- **gVisor `runsc`**: the chart's `SandboxConfig gvisor-default` (`charts/substrate/templates/sandboxconfig-gvisor.yaml`)
  names `gs://gvisor/releases/release/20260803/{x86_64,aarch64}/gvisor.tar.bz2`; atelet downloads the release
  tarball at prewarm. Unchanged from upstream and not mirrored; a cluster needs egress to
  `storage.googleapis.com` from the atelet pods (or an override of `spec.assets` on the SandboxConfig).
- **micro-VM assets** (kata, cloud-hypervisor, virtiofsd): assembled by `hack/microvm-assets/assemble.sh` for the
  e2e suites; the platform does not run the micro-VM sandbox class.

## Contributing

- **Upstream first, prepared here.** Every behavioural change is prepared for upstream
  ([agent-substrate/substrate](https://github.com/agent-substrate/substrate); issues are disabled there) and is one
  upstream-ready commit, DCO-signed (`git commit -s`), with no fork references. The commit goes onto the fork with its
  ledger row in `fork/`, and is pushed as an `upstream/<slug>` branch on this repository, based on the mirror's `main`
  at the pin. Its ledger row reads "to file — prepared in the fork: branch `upstream/<slug>`" until it is opened, and
  links the upstream pull request from then on. A human reviews the prepared branches in batches, decides what goes
  upstream and opens it; an agent never opens, comments on, reviews or closes a pull request or issue on the upstream repository.
  The line carries the same change as a `git cherry-pick -x` of the upstream-ready commit until an upstream release
  contains it, with its ledger row.
- **Public only.** This repository is public: a ledger row, a commit message and this file cite public issues and pull
  requests only, and where the reasoning lives in a private tracker it is written out in a sentence here instead. The
  `fork-ledger` check fails a pull request whose added ledger lines cite a giantswarm repository that is not public
  or a row of a private list (`.github/scripts/fork-public-links.sh`).
- **Fork-only changes** (workflows, this file): a pull request from `fork/<topic>` against `giantswarm`, squash-merged.
  A cherry-pick of an upstream commit is rebase-merged (see "Branches").
- **The ledger**: a pull request against a line that changes more than this file names its own number as `#n` in a
  ledger file of its own (`fork/<change>.md`, or `fork/fork-infrastructure-*.md` for CI and tooling; a maintenance
  branch keeps its own copy of this file as its ledger), or
  carries the label `no-ledger` with a line `no-ledger: <reason>` in its description. The `fork-ledger` workflow
  checks it on every pull request (`docs(fork)` pull requests, re-pin candidates and bot pull requests need no
  ledger entry) and, on every line, that every pull request merged into it has its row in the line's ledger; it
  names each missing number.
- **Experiments**: your own personal fork. Branches here exist to become pull requests.
- **What CI runs on a pull request**: upstream's `pr-workflow` (unit, root-gated and e2e suites on kind, gVisor and
  micro-VM lanes: every suite runs on both, so a node-level arrangement a test makes must hold on both ateoms; the micro-VM ateom clears its checkpoint directory before writing, the gVisor ateom only creates it, so blocking the checkpoint with an immutable file passes one lane and fails the other, while a file under the directory's name is refused by both; see `CONTRIBUTING.md`), `helm-e2e` (the charts on kind), `govulncheck` and `fork-ledger` (the ledger, above); `run-tests` and `govulncheck` are required. `govulncheck` runs once per Go
  module (upstream's matrix since v0.4.0-alpha1) and a job of the line reports the one `govulncheck` status the
  rulesets and the land step require.
  CircleCI's `validate` workflow builds every image for both platforms on a pull request's branch and pushes
  nothing; its `publish` workflow runs on pushes to `giantswarm` and on release tags ("Publishing").
- Upstream's `release.yaml` (dispatch-only; it would have pushed images and charts to ghcr.io under an arbitrary
  tag), `agentgateway-image.yaml` (dispatch-only; it would have built an agentgateway commit and pushed it to
  `ghcr.io/giantswarm/substrate/agentgateway`, while the line runs the agentgateway line's gsoci release) and `release-notes.yaml` (it drafts a GitHub release with upstream's notes on every `v*` tag, which
  `zz_generated.auto_release` owns here) are **removed** on this branch: nothing publishes from GitHub Actions. Do not push tags other than
  `vX.Y.Z-gs.N` releases.
