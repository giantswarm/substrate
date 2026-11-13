# CI release asset caches

The fork's [PR workflow](../../.github/workflows/pr-workflow.yaml) caches
Weaver and envtest; the [Helm workflow](../../.github/workflows/helm-e2e.yaml)
caches helm-unittest. These caches keep transient GitHub release-download
failures from blocking setup when the pinned assets are already available.
The archive installer supports Linux amd64 runners.

## Assets, paths, and keys

All Actions keys start with `release-assets-v1-<runner.os>-<runner.arch>-`.
There are no fallback restore keys.

| Asset | Cached path | Key suffix |
| --- | --- | --- |
| Weaver archive and upstream SHA-256 file | `~/.cache/substrate-release-assets/weaver/<version>/linux-amd64` | `weaver-<version>`, read from `WEAVER_VERSION` in `hack/verify/metrics.sh` |
| envtest installed control-plane binaries | `~/.local/share/kubebuilder-envtest` | `envtest-<selector>-<hash>`, using `kubeVersion` in `internal/testenv/testenv.go` and the hash of `hack/tools/setup-envtest/go.sum` |
| helm-unittest archive and upstream checksum list | `~/.cache/substrate-release-assets/helm-unittest/<version>/linux-amd64` | `helm-unittest-1.0.3`, pinned in the Helm workflow |

The Actions archive paths cover each tool's entire directory above the version
subdirectory. `RELEASE_ASSET_CACHE` supplies the archive cache root to
[install-release-tool.sh](../../.github/scripts/install-release-tool.sh).
Weaver is extracted into the version directory's `bin` and added to
`GITHUB_PATH`; helm-unittest is extracted into
`$RUNNER_TEMP/helm-plugins/helm-unittest` via `HELM_PLUGINS`.
The envtest setup step is followed by `setup-envtest use <selector> -i --print
path`, proving that the selector resolves using installed binaries only.

## Cold downloads and failures

[release-asset.sh](../../.github/scripts/release-asset.sh) reuses a nonempty
cached file. Otherwise, curl downloads into a temporary sibling file and
renames it to the cache destination only on success; failures clean up the
temporary file. Curl retries transient failures up to five times with its
exponential backoff, a 180-second retry window, a 15-second connection timeout,
and a 60-second limit per attempt. The retry window limits when another retry
can start; it is not a strict total wall-clock deadline.

Envtest preparation uses [retry.sh](../../.github/scripts/retry.sh): at most
six command attempts, with waits of 2, 4, 8, 16, and 32 seconds. The final
failure remains a job failure. This wrapper bounds attempts and backoff, not
the duration of the setup command itself.

Both archive installers run `sha256sum -c` before extraction on cold and warm
runs. Weaver uses its archive's upstream `.sha256` file; helm-unittest selects
the matching platform archive from the upstream checksum list and fails if
that entry is absent. A checksum mismatch fails setup without installing the
archive; it does not silently redownload or accept it. Envtest installation
is delegated to `setup-envtest`, rather than this archive checksum helper.
Actions saves these caches only after a successful job.

## Invalidation

Changing a pinned tool version or runner platform selects a different key.
For envtest, changing its version selector or tool lockfile also selects a
new key. A selector does not force a refresh of an already installed matching
version, so invalidate the cache when a fresh resolution is required.

For a corrupt or stale cache, delete the affected Actions cache entry and
rerun the workflow, or bump the `release-assets-v1` key prefix in the affected
workflow. Actions caches are immutable: rerunning with the same restored key
does not replace its contents. For a local archive cache, move aside the
affected version directory (including both archive and checksum files)
before retrying; for envtest, move aside its installed cache directory.
Do not bypass checksum verification to repair a cache.
