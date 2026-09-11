# The Giant Swarm line of Agent Substrate

This repository is Team Bumblebee's fork of [kagent-dev/substrate](https://github.com/kagent-dev/substrate)
(itself a fork of [agent-substrate/substrate](https://github.com/agent-substrate/substrate)). The Giant Swarm
Agent Platform's kagent (API v2) runs every agent as a Substrate actor; this fork is where the platform's
Substrate is pinned, built, scanned and published. It answers one question from one place: **which Substrate
are we running, and why does it differ from upstream?**

Tracking (both upstream repositories and this fork have issues disabled): the line
[giantswarm/giantswarm#37757](https://github.com/giantswarm/giantswarm/issues/37757), upstream engagement
[giantswarm/giantswarm#37742](https://github.com/giantswarm/giantswarm/issues/37742) (Substrate rows), the
kagent line built the same way [giantswarm/giantswarm#37010](https://github.com/giantswarm/giantswarm/issues/37010)
(`giantswarm/kagent-upstream`), the epic [giantswarm/giantswarm#37705](https://github.com/giantswarm/giantswarm/issues/37705).

## Branches

| Branch | What it is | Who moves it |
|---|---|---|
| `main` | A pure mirror of upstream `main`. Upstream rebases its `main` onto agent-substrate (no release tag is an ancestor of it), so the mirror is a **forced** update. Never edited, never the target of a pull request. The mirrored commits carry upstream's workflow files, whose `main` triggers would run upstream's suites here for nothing — the sync cancels those runs right after the push. | the sync workflow (as the HeraldBot App) — the `protect-main` ruleset admits the App and nobody else; a repair is a `workflow_dispatch` of the sync |
| `giantswarm` (default) | **The line**: the upstream release tag the platform's kagent pins ("the pin") + cherry-picked upstream fixes + this fork's own files. Every change of the fork's own files is a pull request against it. Merge method: a cherry-pick of an upstream commit is **rebase-merged**, one commit per patch, so its patch identity survives and `git rebase` drops it by itself once the pin contains it; fork-infrastructure pull requests are squashed. | pull requests (`run-tests` and `govulncheck` required); the sync workflow (the HeraldBot App) and repository admins may force-push it for a re-pin |
| `fork/<topic>` | pull-request branches against `giantswarm` | anyone in the team |
| `sync/<date>-<pin>` | hand-over branches the sync workflow opens when a re-pin conflicts | the sync workflow; a human finishes them |

## Pin

| | |
|---|---|
| Upstream tag | **v0.0.26** (2026-09-06; tag commit `0cb6a535`) |
| Why this one | `giantswarm/kagent-upstream` pins `github.com/kagent-dev/substrate v0.0.26` in `go/go.mod` (the `replace` of `github.com/agent-substrate/substrate`): the ate-api gRPC contract between kagent's client and Substrate's server must match. |
| agentgateway it runs | `AGENTGATEWAY_IMAGE` in `publish.yaml`, equal to the chart default `images.agentgateway` (the publish refuses a drift — the chart must install unstamped, and its egress config is written for this build's schema): a release of the agentgateway line — **`v1.5.1-gs.2`** = upstream v1.5.0 + agentgateway#3237 (every egress CONNECT authorized against ate-api: the actor's UID, then its state) + the line's patch admitting a `RESUMING` actor (giantswarm/agentgateway-upstream#4). Upstream's chart pins `ghcr.io/kagent-dev/substrate/agentgateway:c0f5597c7cb8` at v0.0.26 (a build of upstream agentgateway from 2026-08-30 with no revision label: a pre-merge build of #3237, `RUNNING` only) and `ghcr.io/agentgateway/agentgateway:v0.0.0-alpha.9f9744cf` on `main` (kagent-dev/substrate#28, upstream commit `9f9744cf` — the new substrate ingress header, agentgateway#3409). The line's pin follows: v1.5.0-based releases for this pin, ≥ `9f9744cf` for the first pin containing #28 (the agentgateway line's `FORK.md`, "Convergence with the Substrate line"). |
| When it moves | only together with kagent's pin, proven in agentlab first (`agentlab configure --defaults --chart-branch poc/kagent-main && agentlab up` and the proofs) — see "Re-pin". Not on a schedule. |
| Derived how | `git describe --tags --abbrev=0 --match 'v[0-9]*' --exclude '*-*' giantswarm` with upstream's tags fetched; the line's own tags carry a pre-release suffix and are excluded. The workflows compute it, nothing records it twice. |

## Carried patches

Everything on `giantswarm` that is not in the pin (`git log v0.0.26..giantswarm`):

| Patch | Purpose | Fork commit | Upstream |
|---|---|---|---|
| Grant atelet cluster-wide read access to sandbox configs | atelet's sandbox-asset prewarm degraded on the second test cluster without the RBAC ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 10) | `74b45f9e` (`git cherry-pick -x a7505e9c`) | [kagent-dev/substrate#33](https://github.com/kagent-dev/substrate/pull/33), merged 2026-09-08, not in v0.0.26 — falls away at the re-pin onto the first tag that contains it |
| Let an actor's egress through while it resumes (ateom arms tunneled egress before the first container starts, atenet admits `RESUMING` actors, both hops log a refusal) | an actor whose workload fetches what it needs to become ready — kagent's Go ADK and Claude harnesses materialise git skills before readyz — never got its golden snapshot: atunnel dropped the fetch (`Broken pipe`), atenet would have refused a non-`RUNNING` actor, nothing was logged ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) rows 8 and 13; acceptance test `agentlab skills-test`, [agentlab#137](https://github.com/giantswarm/agentlab/issues/137)) | [#4](https://github.com/giantswarm/substrate/pull/4) (`181762747bb2`; first published as `0.0.27-dev.giantswarm.2026-09-10.22-37-39.h1817627`) | to file: the upstream-shaped patch is branch [`upstream/atenet-egress-during-resume`](https://github.com/giantswarm/substrate/tree/upstream/atenet-egress-during-resume) here (`3a95d7cf`, on the mirror `main`); a team member opens the kagent-dev/substrate pull request with DCO sign-off once #37742 has reviewed it. **Complete with the egress dataplane this pull request pins:** the chart's `images.agentgateway` is the check in the request path (the egress config carries the `substrateEgress` policy and no `ext_proc`, so atenet's handler is not consulted); kagent-dev's `c0f5597c7cb8` (a pre-merge build of agentgateway#3237) authorized every CONNECT against ate-api itself — UID, then `RUNNING` — and refused the golden boot (`atunnel failed to open egress tunnel … 403 Forbidden: actor is not running`, agentlab 2026-09-11); upstream agentgateway v1.5.0 has no such check at all (its `substrateEgress` derives the actor from the SPIFFE id and checks nothing else — `agentlab skills-test` green on both halves with it swapped into `atenet-egress`, 2026-09-11, the run that proved the Substrate half). The line now runs the agentgateway line's `v1.5.1-gs.2`, which keeps #3237's UID and state check and admits `RESUMING` (giantswarm/agentgateway-upstream#4; upstream-facing branch [`upstream/substrate-egress-resuming`](https://github.com/giantswarm/agentgateway-upstream/tree/upstream/substrate-egress-resuming), #37742 row 8). Acceptance test of the combined fix: `agentlab skills-test` on the first build of this merge, recorded on agentlab#137 |
| Declare the egress actor authorization as a frontend policy (`frontendPolicies.substrateEgress` in the atenet-egress config, the route-level policy removed) and pin `images.agentgateway` to the agentgateway line's `v1.5.1-gs.2` | the line's dataplane carries agentgateway#3237, which moved the CONNECT-time actor check from a route policy to a frontend policy; with v0.0.26's route-level shape the gs.2 dataplane refuses its config (`unknown field substrateEgress`, atenet-egress CrashLoopBackOff, agentlab 2026-09-11) and the pin moves with the config because `v1.5.1-gs.1` (v1.5.0) rejects the frontend-level field and the pre-merge build `c0f5597c7cb8` the route-level one only | the `chart: authorize the egress actor as a frontend policy at CONNECT time` commit of pull request #9 | [kagent-dev/substrate#28](https://github.com/kagent-dev/substrate/pull/28) (merged 2026-09-10, on `main`) makes the same move for its `v0.0.0-alpha.988ac151` dataplane under the name agentgateway#3318 gave the policy, `substrateEgressActorResolution`; falls away at the re-pin onto the first tag containing #28 once the agentgateway line carries #3318 (until then the field name differs — resolve by keeping the line's). The e2e install manifests (`manifests/ate-install/components/agentgateway`) still run kagent-dev's `c0f5597c7cb8` with the route-level config, self-consistent; #28 moved them too |
| Read ate-api-server's PostgreSQL connection string from a Secret (`postgres.connectionStringSecretRef`; the `ate-api-server-envvars` ConfigMap then carries only the schema) | meta chart 4.0 puts Substrate's control-plane database on the platform's CNPG cluster and hands ate-api-server the DSN through a Secret, never a ConfigMap ([giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342); [#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 24) | `c1e4e32d` (`git cherry-pick -x 1872249e`) and `f06f5ef9` (`git cherry-pick -x 41097da7`, the `helm plugin install --verify=false` of the same pull request), [#8](https://github.com/giantswarm/substrate/pull/8) | [kagent-dev/substrate#32](https://github.com/kagent-dev/substrate/pull/32), open (2026-09-04), not ours — falls away at the re-pin onto the first release that carries it |
| The atelet DaemonSet takes `nodeSelector`, `tolerations` and `affinity` (`atelet.{nodeSelector,tolerations,affinity}`, empty by default) | the platform pins atelet to worker nodes / node pools; the chart had no scheduling knob ([giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342); [#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 23) | `084d916d`, [#8](https://github.com/giantswarm/substrate/pull/8) | to file: the upstream-shaped patch is branch [`upstream/atelet-scheduling`](https://github.com/giantswarm/substrate/tree/upstream/atelet-scheduling) here (`b34c1690`, on the mirror `main`); [kagent-dev/substrate#16](https://github.com/kagent-dev/substrate/pull/16) touches the same knob (`atelet.nodeSelector`, no tolerations or affinity) inside a fork-wide 92-file pull request that has conflicted since July — align with the maintainers there; a team member opens the kagent-dev/substrate pull request with DCO sign-off once #37742 has reviewed it |
| atelet mounts `/var/lib/kubelet/plugins` with `mountPropagation: HostToContainer` | with the default (private) propagation atelet's mount namespace kept a copy of every CSI globalmount on the node; after a pod moved, the volume's filesystem (and LUKS mapper) stayed open there and Longhorn's `NodeUnstageVolume` failed forever with `luksClose: Device is still in use` — the SPIRE outage of 2026-09-11 on the homelab cluster ([giantswarm/giantswarm#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 30) | `295f3bc6` | to prepare |
| Keep the `ate.dev` CRDs when the `substrate-crds` release is uninstalled (`helm.sh/resource-policy: keep` on the three CRD templates, set at the generator as a `+kubebuilder:metadata:annotations` marker on the root types, so `hack/verify/crd-chart.sh` keeps the templates a verbatim copy) | the chart ships its CRDs as templates, so an uninstall of that release deleted the CRDs and every `WorkerPool`, `SandboxConfig` and `CSIDriverConfig` with them; a GitOps controller that uninstalls the releases concurrently — a cluster's own Flux finalizing the meta chart's component `HelmRelease`s — can remove the CRDs first, and the `substrate` release's uninstall then fails for good on its `SandboxConfig` (`failed to delete release: substrate`, Helm cannot delete an object whose kind is gone; [giantswarm/agent-platform#385](https://github.com/giantswarm/agent-platform/issues/385)). The kagent line carries the same policy on `kagent-crds` (giantswarm/kagent-upstream#10) ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 28). A kept CRD means its objects survive a Substrate uninstall: the consumer deletes its `WorkerPool`s and `SandboxConfig`s, or the CRDs explicitly | `d37ebf63` (as it is on `giantswarm` after the rebase merge), [#12](https://github.com/giantswarm/substrate/pull/12) | to file: the upstream-shaped patch is branch [`upstream/substrate-crds-resource-policy-keep`](https://github.com/giantswarm/substrate/tree/upstream/substrate-crds-resource-policy-keep) here (`41bd9df0`, on the mirror `main`; the pull-request text is in #12); neither kagent-dev/substrate nor agent-substrate/substrate carries or proposes the policy (searched 2026-09-11); a team member opens the kagent-dev/substrate pull request with DCO sign-off once #37742 has reviewed it |
| Fail an actor whose image the registry refuses instead of resuming it forever (the image cache tags a registry's final word — a 4xx other than 408 or 429: manifest or repository unknown, unauthorized, denied — with `ReasonFailedGetExternalObject`; the Run/Restore boundaries claim it; `maybeCrashActor` returns the crash with its cause and the directive; the ActorTemplate reconciler fails the template with that cause when the resume reports the crash) | a Harness whose `workload.image` could not be pulled never booted and was never reported: atelet's pull failed with the registry's answer, the Run RPC returned it untagged, ate-api retried the resume with backoff, `GoldenSnapshotStatus.ErrorMessage` stayed empty, kagent reported `Ready=False ActorTemplatePending` for as long as anyone waited and a worker stayed pinned to the golden actor ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 27; measured in giantswarm/agent's ATS on `v0.0.27-gs.2`: 300 s, no message). Now `Ready=False ActorTemplateFailed` with `GoldenActorCrashed: actor ate-golden/<uid> crashed: … MANIFEST_UNKNOWN: manifest unknown`, in seconds | `bbe92143`, [#14](https://github.com/giantswarm/substrate/pull/14) | to file: the upstream-shaped patch is branch [`upstream/golden-boot-image-pull-terminal`](https://github.com/giantswarm/substrate/tree/upstream/golden-boot-image-pull-terminal) here (`4a56c0af`, the same commit on the mirror `main`; its message is the pull-request text). agent-substrate/substrate#1220 (open since 2026-08-26, review comments unaddressed, needs a rebase, does not apply to v0.0.26) proposes to crash actors on every failure not marked retriable and classifies registry answers with `transport.Error.Temporary()`; its reviewer asked for the status-based classification this patch does for the one class that is a registry's final word — the patch applies on its own and folds into #1220's shape if that lands. A team member opens the kagent-dev/substrate pull request with DCO sign-off once #37742 has reviewed it |
| Keep `podcertificate-controller-system` across an uninstall (`helm.sh/resource-policy: keep` on the chart's `Namespace`; the kubectl-apply manifest and a chart unit test with it) | the namespace holds the two CA pools the podcertificate-controller signs from — provisioned into it out of band (upstream `kubectl-ate admin make-ca-pool`; the platform's connectivity bootstrap hook, which keeps them) — while the signers' `ClusterTrustBundle`s are cluster-scoped and outlive the release. An uninstall took the pools and left the bundles; the reinstall minted new roots, the controller republished the bundles within seconds, but every pod had already read the surviving bundle when it started (`ateapiauth` loads the ate-api CA file once, at dial time) and failed each handshake against ate-api-server with `x509: certificate signed by unknown authority` — no golden boot (the agent-platform ATS own-Flux scenario on the cluster its smoke had uninstalled from, [giantswarm/agent-platform#384](https://github.com/giantswarm/agent-platform/issues/384); [#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 29). Kept, a reinstall signs from the roots the bundles already carry, as `ate-system`'s pools do (`createNamespace: false`) | `e73c1dc0`, [#13](https://github.com/giantswarm/substrate/pull/13) (first release v0.0.27-gs.5) | to file: the upstream-shaped patch is branch [`upstream/keep-podcert-namespace`](https://github.com/giantswarm/substrate/tree/upstream/keep-podcert-namespace) here (`38a4e3a1`, on the mirror `main` @ `007eb1ee`); a team member opens the kagent-dev/substrate pull request with DCO sign-off once #37742 has reviewed it. No upstream issue or pull request covers it (searched kagent-dev/substrate and agent-substrate/substrate, 2026-09-11: agent-substrate#146 and #1166 touch stale CA material in the kind and e2e setups only) |
| Fork infrastructure: this file, the README pointer, `CODEOWNERS`, `.github/workflows/publish.yaml`, `.github/workflows/sync-upstream.yaml`, `.trivyignore`, and the branch triggers of `pr-workflow.yaml`, `helm-e2e.yaml`, `govulncheck.yaml` (`main` → `giantswarm`, govulncheck also on pull requests) | the line's CI, publishing and sync | the `giantswarm` branch history | not for upstream |

Five patches change Substrate ahead of upstream — egress for an actor while it resumes, without which no
skill-carrying agent of the platform boots, the atelet scheduling knobs, the keep policy on the CRD chart's
templates, the kept podcertificate-controller namespace, without which a reinstall on the same cluster has
no working trust chain, and the terminal classification of an image the registry refuses, without which a
misconfigured Harness image is never reported; all five are written for upstream and leave at the first release that carries them. The Postgres Secret patch is upstream's own open pull
request. Everything else is what upstream has already merged. Giant Swarm specific wiring
lives elsewhere: the CA/JWT pool bootstrap (`kubectl-ate admin make-ca-pool`/`make-jwt-pool`
and the `ate-api-authentication` ConfigMap) is created by [agentlab](https://github.com/giantswarm/agentlab)
and by meta chart 4.0; the `WorkerPool` the platform's Harnesses run on comes with the kagent chart
(`kagent.substrateWorkerPool`); feature gates, Kyverno exceptions and network policies are cluster
configuration.

Dropped at the bootstrap of the line (2026-09-10): the three June commits of the old fork `main` — `6564754e`
enable websockets (upstream has it: `cmd/atenet/internal/router/xds.go` `UpgradeConfigs`), `5169cdd4`
ActorTemplate env refs (upstream kagent-dev/substrate#20, merged), `63ea2b0e` a dispatch-only release workflow
that never ran (upstream ships `release.yaml`) — and the eight stale June branches whose patches upstream has
merged.

## Re-pin

The re-pin moves the line onto a new upstream release tag and replays the carried patches; a patch upstream has
merged falls away by itself (`git rebase` drops already-applied patches). It is the one sanctioned rewrite of
`giantswarm`.

1. kagent first: the new pin is whatever `giantswarm/kagent-upstream`'s `go/go.mod` `replace` names after its own
   re-pin. Do not move Substrate ahead of kagent — the ate-api contract is versioned by that pin.
2. Run **Actions → sync-upstream → Run workflow** with `pin` = the tag (for example `v0.0.27`). The workflow
   mirrors `main`, rebases the carried patches onto the tag, runs `go build ./... && go test ./...`, and
   force-pushes `giantswarm`. The push runs upstream's suites (`pr-workflow`, `helm-e2e`, `govulncheck`) and
   `publish` builds the dev build.
   - On a conflict it pushes `sync/<date>-<tag>` (the new tag + the patches that applied before the conflict) and
     opens a pull request that names the conflicting patch and the ones behind it. Finish it by hand: check the
     branch out, `git cherry-pick -x` the rest, resolve, test, `git push --force-with-lease origin HEAD:giantswarm`,
     close the pull request. **Do not merge it** — the line is a rebased branch; a merge would fold the old pin back in.
   - `dry_run: true` does everything except the pushes; the run summary shows the outcome.
3. Update this file (pin, carried patches) in a pull request, and the Substrate rows of #37742.
4. Move the consumers to the new dev version (see "Consumers"), prove it in agentlab, then let the meta chart's
   pin and kagent-upstream follow.

The weekly run (Mondays 05:23 UTC) does not re-pin: it mirrors `main` and **probes** whether the carried patches
still rebase onto upstream `main`, naming the first patch that would conflict in the run summary, so the next
re-pin is never a surprise.

**Identity of the automation.** The workflow pushes as the org's **HeraldBot GitHub App** — a token minted per run
from the org secrets `HERALD_CLIENT_ID` / `HERALD_APP_KEY` (`actions/create-github-app-token`); the App is installed
on every org repository with contents and workflows write access and is a bypass actor (`Integration`) of both
rulesets. Why an App and not the org's machine-account token: GitHub refuses a push from a personal access token
that creates or changes a file under `.github/workflows/` unless the token carries the `workflow` scope, and upstream
`main` — hence every mirror and every re-pin — carries upstream's workflow files; the first run (2026-09-10, with
`TAYLORBOT_GITHUB_ACTION`) failed exactly there. A push with the workflow's own `GITHUB_TOKEN` would not do either: it
triggers no other workflow, and the push to `giantswarm` is what publishes the dev build. Bypass actors of the
rulesets: the App (`Integration` 414149) on both branches, repository admins on `giantswarm` only. Manual fallback
for `main` is a `workflow_dispatch` of the sync; for the line, an admin runs the same commands the workflow runs (the
`main` mirror was bootstrapped by hand on 2026-09-10: `git push --force-with-lease=refs/heads/main:<old> origin
upstream/main:refs/heads/main` — which also started upstream's suites on `main`, hence the cancel step).

Manual equivalent (a workstation, upstream as a remote):

```sh
git fetch upstream main --tags
git checkout giantswarm
git rebase --onto v0.0.27 v0.0.26          # new pin, old pin
go build ./... && go test ./...
git push --force-with-lease origin giantswarm
```

## Publishing

`publish.yaml` publishes to `ghcr.io/giantswarm/substrate` on every push to `giantswarm` and on every `v*` tag;
nothing is ever pushed by hand.

| Artifact | Name |
|---|---|
| Control plane and node images | `ghcr.io/giantswarm/substrate/{ateapi,atecontroller,atelet,atenet,podcertcontroller}:<version>` — linux/amd64 + linux/arm64, built with ko from `./cmd/<name>` on the distroless base `.ko.yaml` pins |
| Worker image | `ghcr.io/giantswarm/substrate/ateom-gvisor:<version>` — the `WorkerPool.spec.workerImage` of the platform's pool |
| agentgateway | not built or mirrored here any more: `images.agentgateway` is stamped to a **release of the Giant Swarm line of agentgateway** — `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:vX.Y.Z-gs.N`, `AGENTGATEWAY_IMAGE` in `publish.yaml` — which atenet-router and atenet-egress run ([giantswarm/agentgateway-upstream `FORK.md`](https://github.com/giantswarm/agentgateway-upstream/blob/giantswarm/FORK.md), tracking [giantswarm/giantswarm#37758](https://github.com/giantswarm/giantswarm/issues/37758)) |
| Charts | `oci://ghcr.io/giantswarm/substrate/helm/substrate-crds:<version>`, `oci://ghcr.io/giantswarm/substrate/helm/substrate:<version>` — `image.registry` and `image.tag` stamped to this registry, `images.agentgateway` to the agentgateway line's release; `version` = `appVersion` = the image tag |

Not published from here: `ateom-microvm` and the demo images (the platform runs gVisor workers only),
`kubectl-ate` binaries (use upstream's release), PyPI packages. Third-party images stay as upstream pins them
(`postgres`, `rustfs`, `amazon/aws-cli`, `coredns`, `busybox`).

**Versions.**

- Image tags of this line carry **no `v`** (ko's convention; upstream's do). The sibling agentgateway line keeps upstream's
  `v` on its image tags (`v1.5.1-gs.1`) because its consumers and the retagger rules carry it — two deliberate choices, do not
  "fix" one to match the other.
- Dev build, on every push to `giantswarm`: `<next upstream patch>-dev.giantswarm.<YYYY-MM-DD>.<HH-MM-SS>.h<sha7>`
  (for the pin v0.0.26: `0.0.27-dev.giantswarm.…`), the schema the kagent line uses — base = the pin's patch + 1,
  branch lowercased to `[a-z0-9-]`, committer date in UTC, so a rebuild of the same commit yields the same version
  and versions sort chronologically within the branch. Consumers that follow the channel use a Flux
  `OCIRepository` with `semver: ">=0.0.27-0 <0.1.0-0"` and `semverFilter: ".*-dev\.giantswarm\..*"`; exact pins
  name the full string.
- Release, on a tag `vX.Y.Z-gs.N` where `X.Y.Z` is upstream's **next** version (the dev base) and `N` counts the
  line's releases of that pin: `v0.0.27-gs.1`. Ordering by semver: `0.0.27-dev.… < 0.0.27-gs.1 < 0.0.27`, so a dev
  build never outranks a release, a fork release never outranks the upstream version it anticipates, and the
  switch to an upstream tag one day is a range change, not a rename. A fleet consumer follows
  `semverFilter: ".*-gs\..*"`.
- `workflow_dispatch` with a `version` input publishes that string (for a one-off).

**Digests.** Every run writes an `Images`/`Charts` table with the digest of each pushed artifact to its summary
and uploads them as the `image-refs` artifact; the platform pins by tag and verifies by digest from there. Release
digests are recorded here:

| Release | Pin | Images and charts |
|---|---|---|
| **v0.0.27-gs.1** (2026-09-11, tag on `213d76b6` = v0.0.26 + #33 + the egress-while-resuming patch (#4) + the frontend-policy egress config and the agentgateway line's `v1.5.1-gs.2` dataplane (#7, #9)) | v0.0.26 | images `ateapi` `sha256:70545853…`, `atecontroller` `sha256:5f1ba422…`, `atelet` `sha256:54a7285c…`, `atenet` `sha256:db1adb6d…`, `podcertcontroller` `sha256:eca30364…`, `ateom-gvisor` `sha256:b6a59a48…` (linux/amd64 + arm64); charts `substrate` `sha256:0742fdca…`, `substrate-crds` `sha256:9d3fc4be…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2` |
| **v0.0.27-gs.2** (2026-09-11, tag on `ef304330` = gs.1 + #8: ate-api-server's PostgreSQL connection string from a Secret (`c1e4e32d`, `f06f5ef9` — the cherry-picks of kagent-dev/substrate#32) and the atelet scheduling knobs (`084d916d`), with their ledger rows) | v0.0.26 | images `ateapi` `sha256:201ef762…`, `atecontroller` `sha256:0e0c7f7c…`, `atelet` `sha256:00815e41…`, `atenet` `sha256:e5a9c2c0…`, `podcertcontroller` `sha256:828b1d14…`, `ateom-gvisor` `sha256:cca86090…` (linux/amd64 + arm64); charts `substrate` `sha256:ea0bcbae…`, `substrate-crds` `sha256:e2188cd0…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34554981306](https://github.com/giantswarm/substrate/actions/runs/34554981306), every scan clean |
| **v0.0.27-gs.3** (2026-09-11, annotated tag on `92eec3c6` = gs.2 + #12: `helm.sh/resource-policy: keep` on the three `ate.dev` CRD templates (`d37ebf63`), with its ledger row) | v0.0.26 | images `ateapi` `sha256:3949f485…`, `atecontroller` `sha256:39307b3c…`, `atelet` `sha256:b3abed76…`, `atenet` `sha256:22a408c7…`, `podcertcontroller` `sha256:23e2c76e…`, `ateom-gvisor` `sha256:f6863171…` (linux/amd64 + arm64); charts `substrate` `sha256:3883f355…`, `substrate-crds` `sha256:534b3276…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34603612441](https://github.com/giantswarm/substrate/actions/runs/34603612441), every scan clean. Consumer: the agent-platform meta chart re-pins to it (giantswarm/agent-platform#390) |
| **v0.0.27-gs.4** (2026-09-11, tag on `bbe92143` = gs.3 + #14: an actor whose image the registry refuses is crashed with the registry's answer and its template's golden boot fails with the cause (`bbe92143`)) | v0.0.26 | images `ateapi` `sha256:023a7b2a…`, `atecontroller` `sha256:7fec05e9…`, `atelet` `sha256:5fcbd743…`, `atenet` `sha256:dc10bb95…`, `podcertcontroller` `sha256:a7b743b7…`, `ateom-gvisor` `sha256:6cb5ba54…` (linux/amd64 + arm64); charts `substrate` `sha256:89d713f1…`, `substrate-crds` `sha256:a6faad30…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34604690599](https://github.com/giantswarm/substrate/actions/runs/34604690599), every scan clean |
| **v0.0.27-gs.5** (2026-09-11, annotated tag on `e47f3c80` = gs.4 + #13: `helm.sh/resource-policy: keep` on the chart's `podcertificate-controller-system` Namespace (`e73c1dc0`), with its ledger row) | v0.0.26 | images `ateapi` `sha256:689f4529…`, `atecontroller` `sha256:85a4a5e3…`, `atelet` `sha256:06226ce2…`, `atenet` `sha256:51c2f20e…`, `podcertcontroller` `sha256:a822055d…`, `ateom-gvisor` `sha256:b989620a…` (linux/amd64 + arm64); charts `substrate` `sha256:99d75b44…`, `substrate-crds` `sha256:70f6a66f…`; dataplane `ghcr.io/giantswarm/agentgateway-upstream/agentgateway:v1.5.1-gs.2`; [run 34607610137](https://github.com/giantswarm/substrate/actions/runs/34607610137), every scan clean |

**Scans.** Every own image is scanned with Trivy (HIGH and CRITICAL, fixable only) after the push and before the
charts that reference it are published. A fixable finding fails the publish: bump the module (upstream first) or,
when upstream has no fix, add a time-boxed entry to `.trivyignore` (`CVE-… exp:YYYY-MM-DD # reason, tracking
issue`) — an expired entry fails again and is re-triaged, not extended. The agentgateway image is scanned
report-only here: it is the agentgateway line's build, its scan gates its own publish and a finding is fixed there. `govulncheck` covers the Go module graph on every push, pull
request and weekly.

## Consumers

| Consumer | Where the pin lives | Selects |
|---|---|---|
| [agentlab](https://github.com/giantswarm/agentlab) | `internal/lab/substrate.go` (`substrateChartsRepo`, `substrateImageRegistry`, `substrateVersion`) | an exact dev version or release; installs `substrate-crds` + `substrate` and preloads the worker image |
| agent-platform meta chart 4.0 (`components.substrate` / `components.substrate-crds`, the `substrate:` values block; [giantswarm/agent-platform#342](https://github.com/giantswarm/agent-platform/issues/342)) | `helm/agent-platform/values.yaml`: the two components' version pins and `kagent.substrateWorkerPool.workerImage` | the `substrate-crds` + `substrate` charts at an exact dev version or release and the `ateom-gvisor` image at the same version (the `WorkerPool` the kagent chart renders) |
| [giantswarm/kagent-upstream](https://github.com/giantswarm/kagent-upstream) | `Makefile` `SUBSTRATE_REPO ?= oci://ghcr.io/giantswarm/substrate/helm`, `SUBSTRATE_VERSION` | the `substrate`/`substrate-crds` chart dependencies of the kagent charts (off in the platform, which installs Substrate as cluster infrastructure) |

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
- **Experiments**: your own personal fork. Branches here exist to become pull requests.
- **What CI runs on a pull request**: upstream's `pr-workflow` (unit, root-gated and e2e suites on kind, gVisor and
  micro-VM lanes), `helm-e2e` (the charts on kind) and `govulncheck`; `run-tests` and `govulncheck` are required.
  `publish` runs only on the branch and on tags.
- **Do not** dispatch upstream's `release.yaml` here (it is the mirror's file; it would push to this registry under
  an arbitrary tag and try to push charts to upstream's), and do not push tags other than `vX.Y.Z-gs.N` releases.
