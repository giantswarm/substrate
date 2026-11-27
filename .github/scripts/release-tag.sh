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

# Fork (giantswarm/substrate): one commit, one version. A release tag
# (`vX.Y.Z` or `vX.Y.Z-rc.N`) is refused when its commit already carries the
# release tag of another version, so a commit never answers "which version is
# this" twice (FORK.md, "Versions"). A candidate and its stable share a commit
# by design: tags of the same `vX.Y.Z` never conflict. Tags outside the
# release pattern (upstream's prereleases on the pin) are not counted.
#
#   release-tag.sh <tag>
#
# Reads the tags of the repository in the working directory; fetch them first.
# Exit 1 names the tag, its commit and every conflicting tag; exit 2 is a usage
# error (no release tag, or no such tag).

set -o errexit -o nounset -o pipefail

release='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'

[[ $# -eq 1 && "$1" =~ ${release} ]] || {
  echo "usage: $0 <vX.Y.Z or vX.Y.Z-rc.N>" >&2
  exit 2
}
tag="$1"
commit="$(git rev-parse --verify --quiet "refs/tags/${tag}^{commit}")" || {
  echo "release-tag: no tag ${tag} in this repository" >&2
  exit 2
}
version="${tag%%-rc.*}"

conflicts=()
while IFS= read -r other; do
  [[ "${other}" =~ ${release} && "${other%%-rc.*}" != "${version}" ]] && conflicts+=("${other}")
done < <(git tag --points-at "${commit}")

if [[ ${#conflicts[@]} -eq 0 ]]; then
  echo "release-tag: ${tag} is the only version on ${commit:0:8}"
  exit 0
fi
echo "ERROR: release tag ${tag} is on commit ${commit:0:8}, which already carries release tag ${conflicts[*]}: one commit releases one version. Release the change from a commit of its own; the published version of ${commit:0:8} stays as it is." >&2
exit 1
