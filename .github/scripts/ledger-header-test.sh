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

# Fork (giantswarm/substrate): fixture runs of ledger-header.sh, one ledger
# directory per case.

set -o errexit -o nounset -o pipefail

CHECK="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ledger-header.sh"
DIR="$(mktemp -d)"
trap 'rm -rf "${DIR}"' EXIT

failures=0

good() {
  cat <<'EOF'
---
id: a-good-row
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1543, giantswarm/substrate#12]
  exit: 'as#1543 merged; the line''s patch drops'
depends_on: [another-row, giantswarm/substrate:let-actors-egress-through-while-resumes, agent-substrate/substrate#2117]
commit: 52f1c49a
verified: 2026-10-19
---
# A good row

- **Status:** carried
EOF
}

# case <name> <want exit code> <file name> <file text> [text the output must name]
case_() {
  local name="$1" want="$2" file="$3" text="$4" names="${5:-}" got=0 out ledger
  ledger="${DIR}/${name// /-}"
  mkdir -p "${ledger}"
  good >"${ledger}/a-good-row.md"
  good | sed 's/^id: a-good-row$/id: another-row/' >"${ledger}/another-row.md"
  printf '%s\n' "${text}" >"${ledger}/${file}"
  out="$("${CHECK}" substrate "${ledger}" 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]] || [[ -n "${names}" && "${out}" != *"${names}"* ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}${names:+ naming ${names}}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

case_ "a good ledger" 0 "third-row.md" "$(good | sed 's/^id: a-good-row$/id: third-row/')"
case_ "no header" 1 "bare.md" "# Bare" "no header"
case_ "never closed" 1 "open-ended.md" "$(printf -- '---\nid: open-ended\nfork: substrate\n')" "never closes"
case_ "id names another file" 1 "wrong-id.md" "$(good)" 'does not match the file name "wrong-id"'
case_ "the other fork" 1 "other-fork.md" "$(good | sed 's/^id: a-good-row$/id: other-fork/; s/^fork: .*/fork: elsewhere/')" 'fork "elsewhere"'
case_ "a theme off the catalogue" 1 "off-theme.md" "$(good | sed 's/^id: a-good-row$/id: off-theme/; s/^theme: .*/theme: misc/')" 'theme "misc"'
case_ "a category off the list" 1 "off-category.md" "$(good | sed 's/^id: a-good-row$/id: off-category/; s/^category: .*/category: chore/')" 'category "chore"'
case_ "an outcome off the list" 1 "off-outcome.md" "$(good | sed 's/^id: a-good-row$/id: off-outcome/; s/^  outcome: .*/  outcome: pending/')" 'upstream.outcome "pending"'
case_ "a missing key" 1 "no-verified.md" "$(good | sed 's/^id: a-good-row$/id: no-verified/; /^verified:/d')" "verified is missing"
case_ "a bad date" 1 "bad-date.md" "$(good | sed 's/^id: a-good-row$/id: bad-date/; s/^verified: .*/verified: yesterday/')" 'verified "yesterday"'
case_ "a bare reference" 1 "bare-ref.md" "$(good | sed 's/^id: a-good-row$/id: bare-ref/; s/^  refs: .*/  refs: [#12]/')" 'upstream.refs "#12"'
case_ "the internal tracker" 1 "tracker.md" "$(good | sed 's/^id: a-good-row$/id: tracker/; s/^  refs: .*/  refs: [giantswarm\/giantswarm#37742]/')" "internal tracker"
case_ "an exit missing while open" 1 "no-exit.md" "$(good | sed 's/^id: a-good-row$/id: no-exit/; /^  exit:/d')" "upstream.exit is empty while outcome is open"
case_ "an exit missing while merged" 0 "merged.md" "$(good | sed 's/^id: a-good-row$/id: merged/; s/^  outcome: .*/  outcome: merged/; /^  exit:/d')"
case_ "a dependency on no file" 1 "no-dep.md" "$(good | sed 's/^id: a-good-row$/id: no-dep/; s/^depends_on: .*/depends_on: [missing-row]/')" 'depends_on "missing-row" names no file'
case_ "a malformed dependency" 1 "bad-dep.md" "$(good | sed 's/^id: a-good-row$/id: bad-dep/; s/^depends_on: .*/depends_on: [Not A Row]/')" 'depends_on "Not A Row"'
case_ "an empty commit" 0 "no-commit.md" "$(good | sed 's/^id: a-good-row$/id: no-commit/; s/^commit: .*/commit: ""/')"
case_ "a short commit" 1 "short-commit.md" "$(good | sed 's/^id: a-good-row$/id: short-commit/; s/^commit: .*/commit: abc12/')" 'commit "abc12"'
case_ "a pull request as the commit" 0 "pr-commit.md" "$(good | sed 's/^id: a-good-row$/id: pr-commit/; s/^commit: .*/commit: giantswarm\/substrate#7/')"
case_ "a quoted commit" 0 "quoted-commit.md" "$(good | sed 's/^id: a-good-row$/id: quoted-commit/; s/^commit: .*/commit: "08779062"/')"
case_ "a table below the header" 0 "table-row.md" "$(good | sed 's/^id: a-good-row$/id: table-row/; /^# A good row/,$d'; printf '| Carried commit (subject) | Why the platform needs it | Upstream |\n|---|---|---|\n| `fix(x): y` | because | none |\n')"

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
