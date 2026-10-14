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

# Fork (giantswarm/substrate): a pull request that ships a change (title type
# feat, fix, perf, refactor or security) carries its docs/upgrade.md entry in
# the same pull request, or says in its description why it needs none. The
# release tooling (cliff.toml) skips docs, test and style commits, so an entry
# that lands alone never cuts a tag: the releasable change that cuts it has to
# carry the entry (docs/dev/release-notes.md, "Cut a release").
#
#   upgrade-entry.sh <pull-request.json>
#
# The JSON is what `gh pr view --json number,title,body,files,headRefName,author`
# prints. A pull request needs no entry when its type is not one of the five
# above (chore, ci, build, docs, test, style), it comes from a re-pin candidate
# (`sync/…`), a bot opened it, or its description has a line
# `no-upgrade-entry: <reason>`. Exit 1 names the pull request.

set -o errexit -o nounset -o pipefail

[[ $# -eq 1 && -f "$1" ]] || {
  echo "usage: $0 <pull-request.json>" >&2
  exit 2
}

number="$(jq -r '.number' "$1")"
missing="$(jq -r '
  select((.headRefName // "") | startswith("sync/") | not)
  | select(.author.is_bot // false | not)
  | select((.title // "") | test("^(feat|fix|perf|refactor|security)(\\([^)]*\\))?!?:"))
  | select([.files[]?.path] | index("docs/upgrade.md") | not)
  | select((.body // "") | test("(?m)^\\s*no-upgrade-entry:\\s*\\S") | not)
  | .number' "$1")"

if [[ -z "${missing}" ]]; then
  echo "upgrade-entry: #${number} carries its docs/upgrade.md entry or needs none"
  exit 0
fi

echo "::error file=docs/upgrade.md::#${missing} changes what ships but not docs/upgrade.md: add the entry for its version step to this pull request, or add a line \"no-upgrade-entry: <reason>\" to its description"
echo "upgrade-entry: #${missing} has no docs/upgrade.md entry" >&2
exit 1
