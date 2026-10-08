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

# Fork (giantswarm/substrate): fixture runs of fork-ledger.sh, each case a
# pull request (or a line's merged ones) against a small ledger.

set -o errexit -o nounset -o pipefail

LINT="$(dirname "${BASH_SOURCE[0]}")/fork-ledger.sh"
DIR="$(mktemp -d)"
trap 'rm -rf "${DIR}"' EXIT

cat >"${DIR}/FORK.md" <<'EOF'
| Patch | Purpose | Fork commit | Upstream |
|---|---|---|---|
| a patch | why | `0123abcd`, [#7](https://github.com/giantswarm/substrate/pull/7) | agent-substrate/substrate#9 |
| another | why | `release-1.3`: #12; `giantswarm`: https://github.com/giantswarm/substrate/pull/30 | none |
EOF

failures=0

# pr <name> <want exit code> <JSON> [text the output must name]
pr() {
  local name="$1" want="$2" json="$3" names="${4:-}" got=0 out
  printf '%s\n' "${json}" >"${DIR}/pr.json"
  out="$("${LINT}" "${MODE:-pr}" "${DIR}/pr.json" "${DIR}/FORK.md" 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]] || [[ -n "${names}" && "${out}" != *"${names}"* ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}${names:+ naming ${names}}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

code='[{"path":"cmd/x.go"},{"path":"FORK.md"}]'

pr "row as a link" 0 '{"number":7,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}'
pr "row as #n" 0 '{"number":12,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}'
pr "row as a bare URL" 0 '{"number":30,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}'
pr "no row" 1 '{"number":8,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}' "#8 is missing"
pr "a prefix of a row is no row" 1 '{"number":1,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}' "#1 is missing"
pr "another repository's number is no row" 1 '{"number":9,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}' "#9 is missing"
pr "FORK.md alone" 0 '{"number":40,"title":"fix: x","body":"","labels":[],"files":[{"path":"FORK.md"}],"headRefName":"fork/x","author":{"is_bot":false}}'
pr "docs(fork)" 0 '{"number":41,"title":"docs(fork): the release table","body":"","labels":[],"files":[{"path":"FORK.md"},{"path":"README.md"}],"headRefName":"fork/x","author":{"is_bot":false}}'
pr "re-pin candidate" 0 '{"number":42,"title":"Re-pin onto v0.4.0","body":"","labels":[],"files":'"${code}"',"headRefName":"sync/2026-10-08-v0.4.0","author":{"is_bot":false}}'
pr "bot" 0 '{"number":43,"title":"chore: align files","body":"","labels":[],"files":'"${code}"',"headRefName":"teams-alignment-branch","author":{"is_bot":true}}'
pr "no-ledger with a reason" 0 '{"number":44,"title":"ci: x","body":"Problem\n\nno-ledger: a CI timeout, no patch\n","labels":[{"name":"no-ledger"}],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}'
pr "no-ledger without a reason" 1 '{"number":45,"title":"ci: x","body":"no-ledger:\n","labels":[{"name":"no-ledger"}],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}' "#45 is missing from the ledger (labelled no-ledger"
pr "a reason without the label" 1 '{"number":46,"title":"ci: x","body":"no-ledger: a reason","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}}' "#46 is missing"

MODE=line pr "line: all merged have rows" 0 '[{"number":7,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}},{"number":40,"title":"docs(fork): x","body":"","labels":[],"files":[{"path":"FORK.md"}],"headRefName":"fork/y","author":{"is_bot":false}}]'
MODE=line pr "line: names every missing number" 1 '[{"number":7,"title":"fix: x","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/x","author":{"is_bot":false}},{"number":8,"title":"fix: y","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/y","author":{"is_bot":false}},{"number":11,"title":"fix: z","body":"","labels":[],"files":'"${code}"',"headRefName":"fork/z","author":{"is_bot":false}}]' "missing #8 #11"
MODE=line pr "line: an empty line" 0 '[]'

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
