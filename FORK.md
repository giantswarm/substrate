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
| `main` | A pure mirror of upstream `main`. Upstream rebases its `main` onto agent-substrate (no release tag is an ancestor of it), so the mirror is a **forced** update. Never edited, never the target of a pull request. | the sync workflow (taylorbot) only — the `protect-main` ruleset admits the `bots` team and nobody else |
| `giantswarm` (default) | **The line**: the upstream release tag the platform's kagent pins ("the pin") + cherry-picked upstream fixes + this fork's own files. Every change of the fork's own files is a pull request against it. | pull requests (`run-tests` and `govulncheck` required); the sync workflow and repository admins may force-push it for a re-pin |
| `fork/<topic>` | pull-request branches against `giantswarm` | anyone in the team |
| `sync/<date>-<pin>` | hand-over branches the sync workflow opens when a re-pin conflicts | the sync workflow; a human finishes them |

## Pin

| | |
|---|---|
| Upstream tag | **v0.0.26** (2026-09-06; tag commit `0cb6a535`) |
| Why this one | `giantswarm/kagent-upstream` pins `github.com/kagent-dev/substrate v0.0.26` in `go/go.mod` (the `replace` of `github.com/agent-substrate/substrate`): the ate-api gRPC contract between kagent's client and Substrate's server must match. |
| When it moves | only together with kagent's pin, proven in agentlab first (`agentlab configure --defaults --chart-branch poc/kagent-main && agentlab up` and the proofs) — see "Re-pin". Not on a schedule. |
| Derived how | `git describe --tags --abbrev=0 --match 'v[0-9]*' --exclude '*-*' giantswarm` with upstream's tags fetched; the line's own tags carry a pre-release suffix and are excluded. The workflows compute it, nothing records it twice. |

## Carried patches

Everything on `giantswarm` that is not in the pin (`git log v0.0.26..giantswarm`):

| Patch | Purpose | Fork commit | Upstream |
|---|---|---|---|
| Grant atelet cluster-wide read access to sandbox configs | atelet's sandbox-asset prewarm degraded on the second test cluster without the RBAC ([#37742](https://github.com/giantswarm/giantswarm/issues/37742) row 10) | `74b45f9e` (`git cherry-pick -x a7505e9c`) | [kagent-dev/substrate#33](https://github.com/kagent-dev/substrate/pull/33), merged 2026-09-08, not in v0.0.26 — falls away at the re-pin onto the first tag that contains it |
| Fork infrastructure: this file, the README pointer, `.github/CODEOWNERS`, `.github/workflows/publish.yaml`, `.github/workflows/sync-upstream.yaml`, `.trivyignore`, and the branch triggers of `pr-workflow.yaml`, `helm-e2e.yaml`, `govulncheck.yaml` (`main` → `giantswarm`, govulncheck also on pull requests) | the line's CI, publishing and sync | the `giantswarm` branch history | not for upstream |

Nothing in the line changes Substrate's behaviour beyond what upstream has already merged. Giant Swarm
specific wiring lives elsewhere: the CA/JWT pool bootstrap (`kubectl-ate admin make-ca-pool`/`make-jwt-pool`
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
| agentgateway | `ghcr.io/giantswarm/substrate/agentgateway:<upstream tag>` — a digest-true `crane copy` of upstream's `images.agentgateway` (atenet-router and atenet-egress run it; this repository does not build it) |
| Charts | `oci://ghcr.io/giantswarm/substrate/helm/substrate-crds:<version>`, `oci://ghcr.io/giantswarm/substrate/helm/substrate:<version>` — `image.registry`, `image.tag` and `images.agentgateway` stamped to this registry; `version` = `appVersion` = the image tag |

Not published from here: `ateom-microvm` and the demo images (the platform runs gVisor workers only),
`kubectl-ate` binaries (use upstream's release), PyPI packages. Third-party images stay as upstream pins them
(`postgres`, `rustfs`, `amazon/aws-cli`, `coredns`, `busybox`).

**Versions.**

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
| none yet | | |

**Scans.** Every own image is scanned with Trivy (HIGH and CRITICAL, fixable only) after the push and before the
charts that reference it are published. A fixable finding fails the publish: bump the module (upstream first) or,
when upstream has no fix, add a time-boxed entry to `.trivyignore` (`CVE-… exp:YYYY-MM-DD # reason, tracking
issue`) — an expired entry fails again and is re-triaged, not extended. The mirrored agentgateway image is
scanned report-only; its findings belong upstream. `govulncheck` covers the Go module graph on every push, pull
request and weekly.

## Consumers

| Consumer | Where the pin lives | Selects |
|---|---|---|
| [agentlab](https://github.com/giantswarm/agentlab) | `internal/lab/substrate.go` (`substrateChartsRepo`, `substrateImageRegistry`, `substrateVersion`) | an exact dev version or release; installs `substrate-crds` + `substrate` and preloads the worker image |
| agent-platform meta chart, branch `poc/kagent-main` | `helm/agent-platform/values.yaml` `kagent.substrateWorkerPool.workerImage` | the `ateom-gvisor` image at an exact version (the `WorkerPool` the kagent chart renders) |
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
- **Fork-only changes** (workflows, this file): a pull request from `fork/<topic>` against `giantswarm`.
- **Experiments**: your own personal fork. Branches here exist to become pull requests.
- **What CI runs on a pull request**: upstream's `pr-workflow` (unit, root-gated and e2e suites on kind, gVisor and
  micro-VM lanes), `helm-e2e` (the charts on kind) and `govulncheck`; `run-tests` and `govulncheck` are required.
  `publish` runs only on the branch and on tags.
- **Do not** dispatch upstream's `release.yaml` here (it is the mirror's file; it would push to this registry under
  an arbitrary tag and try to push charts to upstream's), and do not push tags other than `vX.Y.Z-gs.N` releases.
