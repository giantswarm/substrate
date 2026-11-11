#!/usr/bin/env bash

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# These release archives target the Linux x86-64 hosted CI runners.
set -o errexit -o nounset -o pipefail
root="$(git rev-parse --show-toplevel)"
tool="$1"
version="$2"
cache="${RELEASE_ASSET_CACHE:?}/${tool}/${version}/linux-amd64"
mkdir -p "$cache"
cd "$cache"
case "$tool" in
  weaver)
    archive=weaver-x86_64-unknown-linux-gnu.tar.xz
    base="https://github.com/open-telemetry/weaver/releases/download/${version}"
    "$root/.github/scripts/release-asset.sh" "$base/$archive" "$cache/$archive"
    "$root/.github/scripts/release-asset.sh" "$base/$archive.sha256" "$cache/$archive.sha256"
    sha256sum -c "$archive.sha256"
    mkdir -p "$cache/bin"
    tar -xJf "$archive" --strip-components=1 -C "$cache/bin" "${archive%.tar.xz}/weaver"
    echo "$cache/bin" >> "${GITHUB_PATH:?}"
    "$cache/bin/weaver" --version
    ;;
  helm-unittest)
    archive="helm-unittest-linux-amd64-${version}.tgz"
    base="https://github.com/helm-unittest/helm-unittest/releases/download/v${version}"
    "$root/.github/scripts/release-asset.sh" "$base/$archive" "$cache/$archive"
    "$root/.github/scripts/release-asset.sh" "$base/helm-unittest-checksum.sha" "$cache/checksums"
    # Check only this platform's archive, with the upstream checksum intact.
    awk -v file="$archive" '$2 == "*./_dist/" file {print $1 "  " file}' checksums > selected.sha256
    test -s selected.sha256
    sha256sum -c selected.sha256
    mkdir -p "${HELM_PLUGINS:?}/helm-unittest"
    tar -xzf "$archive" -C "$HELM_PLUGINS/helm-unittest"
    helm unittest --help
    ;;
  *) echo "Unknown release tool: $tool" >&2; exit 1 ;;
esac
