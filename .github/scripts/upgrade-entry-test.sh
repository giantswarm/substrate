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

# Fork (giantswarm/substrate): fixture runs of upgrade-entry.sh, each case a
# pull request.

set -o errexit -o nounset -o pipefail

CHECK="$(dirname "${BASH_SOURCE[0]}")/upgrade-entry.sh"
DIR="$(mktemp -d)"
trap 'rm -rf "${DIR}"' EXIT

failures=0

# case <name> <want exit code> <title> <body> <files JSON> [headRefName] [is_bot]
case_() {
  local name="$1" want="$2" title="$3" body="$4" files="$5" head="${6:-fix/x}" bot="${7:-false}" got=0 out
  jq -n --arg title "${title}" --arg body "${body}" --argjson files "${files}" --arg head "${head}" --argjson bot "${bot}" \
    '{number: 7, title: $title, body: $body, files: $files, headRefName: $head, author: {is_bot: $bot}}' >"${DIR}/pr.json"
  out="$("${CHECK}" "${DIR}/pr.json" 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

code='[{"path":"cmd/x.go"}]'
withentry='[{"path":"cmd/x.go"},{"path":"docs/upgrade.md"}]'

case_ "fix with its entry" 0 "fix(ate-api): x" "" "${withentry}"
case_ "fix without an entry" 1 "fix(ate-api): x" "" "${code}"
case_ "feat without an entry" 1 "feat: x" "" "${code}"
case_ "breaking fix without an entry" 1 "fix(api)!: x" "" "${code}"
case_ "fix with a reason" 0 "fix: x" $'Problem\n\nno-upgrade-entry: a test helper, nothing persisted\n' "${code}"
case_ "an empty reason" 1 "fix: x" $'no-upgrade-entry:\n' "${code}"
case_ "chore" 0 "chore(deps): x" "" "${code}"
case_ "ci" 0 "ci: x" "" "${code}"
case_ "docs" 0 "docs(upgrade): x" "" '[{"path":"docs/upgrade.md"}]'
case_ "re-pin candidate" 0 "fix: x" "" "${code}" "sync/2026-10-08-v0.4.0"
case_ "bot" 0 "fix: x" "" "${code}" "renovate/x" true

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
