#!/usr/bin/env bash

# Copyright 2026 The Agent Substrate Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Fork (giantswarm/substrate): prints the line's pin (FORK.md, "Pin"): the
# nearest upstream release tag reachable from <rev> (default HEAD),
# prereleases included, nearest by commit distance (a version sort would place
# a prerelease behind its release), without its `upstream/` prefix. Upstream's
# tags are `refs/tags/upstream/<tag>` (the sync mirrors them); the bare `v*`
# tags are the line's own releases and never count. Exit 1 when no upstream
# tag is reachable.
#
#   fork-pin.sh [<rev>]   # e.g. v0.5.0-alpha2

set -o errexit -o nounset -o pipefail

rev="${1:-HEAD}"
pin="$(git for-each-ref --merged "${rev}" --format='%(refname:strip=2)' 'refs/tags/upstream/v[0-9]*' |
  while read -r tag; do echo "$(git rev-list --count "refs/tags/${tag}..${rev}") ${tag}"; done |
  sort -k1,1n -k2,2 | awk 'NR == 1 { print $2 }')"
if [[ -z "${pin}" ]]; then
  echo "fork-pin: no upstream release tag (refs/tags/upstream/v*) is reachable from ${rev}" >&2
  exit 1
fi
echo "${pin#upstream/}"
