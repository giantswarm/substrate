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

# Fork (giantswarm/substrate): the ledger cites public repositories only. This
# repository is public, so FORK.md and fork/ link no issue, pull request or
# file of a private repository: a reason that lives there is written out in a
# sentence. The workflow fork-ledger pipes in the lines a pull request adds to
# the ledger (on a push, the whole ledger).
#
#   fork-public-links.sh <ledger-text
#
# A citation is a github.com/giantswarm/<repo> URL or a giantswarm/<repo>#<n>
# reference. giantswarm/giantswarm is internal; every other cited repository
# is looked up once with `gh api`, and one that is not public, or not found
# (a private repository is not found with the workflow's token), fails.

set -o errexit -o nounset -o pipefail

declare -A visibility=([giantswarm]=internal)
failures=0

while IFS= read -r line; do
  repos="$(grep -oE 'github\.com/giantswarm/[A-Za-z0-9_.-]+|giantswarm/[A-Za-z0-9_.-]+#[0-9]+' <<<"${line}" |
    sed -E 's#^(github\.com/)?giantswarm/##; s/#[0-9]+$//; s/\.git$//; s/\.+$//' | sort -u || true)"
  for repo in ${repos}; do
    if [[ -z "${visibility[${repo}]:-}" ]]; then
      visibility[${repo}]="$(gh api "repos/giantswarm/${repo}" --jq .visibility 2>/dev/null)" || visibility[${repo}]="not found"
    fi
    if [[ "${visibility[${repo}]}" != public ]]; then
      echo "::error::the ledger cites giantswarm/${repo} (${visibility[${repo}]}): this repository is public, so write the reason out in a sentence or cite a public issue (FORK.md, Contributing): ${line}"
      failures=$((failures + 1))
    fi
  done
done

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} citation(s) of a repository that is not public"
  exit 1
fi
echo "every cited giantswarm repository is public"
