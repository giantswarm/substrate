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
# directory per case; the completeness cases run against a throwaway git
# repository with a `main` and a `giantswarm` branch.

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
commits:
  - 'fix(atenet): let an actor''s egress through'
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1543, giantswarm/substrate#12]
  exit: 'as#1543 merged; the line''s patch drops'
depends_on: [another-row, giantswarm/substrate:let-actors-egress-through-while-resumes, agent-substrate/substrate#2117]
verified: 2026-10-19
---
# A good row

## Change

What it does.

## Reason

Why the line carries it.

## History

as#1543 open since 2026-09-08; `6be3d50c` ([#4](https://github.com/giantswarm/substrate/pull/4))
EOF
}

# The second fixture file names a subject of its own.
other() {
  good | sed "s/^id: a-good-row$/id: another-row/; s/^  - 'fix(atenet).*/  - 'fix(ateom): arm the tunnel first'/"
}

# case <name> <want exit code> <file name> <file text> [text the output must name]
case_() {
  local name="$1" want="$2" file="$3" text="$4" names="${5:-}" got=0 out ledger
  ledger="${DIR}/${name// /-}"
  mkdir -p "${ledger}"
  good >"${ledger}/a-good-row.md"
  other >"${ledger}/another-row.md"
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

third() { good | sed "s/^id: a-good-row$/id: third-row/; s/^  - 'fix(atenet).*/  - 'fix(x): a third subject'/"; }

case_ "a good ledger" 0 "third-row.md" "$(third)"
case_ "no header" 1 "bare.md" "# Bare" "no header"
case_ "never closed" 1 "open-ended.md" "$(printf -- '---\nid: open-ended\nfork: substrate\n')" "never closes"
case_ "id names another file" 1 "wrong-id.md" "$(third | sed 's/^id: third-row$/id: a-good-row/')" 'does not match the file name "wrong-id"'
case_ "the other fork" 1 "other-fork.md" "$(third | sed 's/^id: third-row$/id: other-fork/; s/^fork: .*/fork: elsewhere/')" 'fork "elsewhere"'
case_ "a theme off the catalogue" 1 "off-theme.md" "$(third | sed 's/^id: third-row$/id: off-theme/; s/^theme: .*/theme: misc/')" 'theme "misc"'
case_ "a category off the list" 1 "off-category.md" "$(third | sed 's/^id: third-row$/id: off-category/; s/^category: .*/category: chore/')" 'category "chore"'
case_ "an outcome off the list" 1 "off-outcome.md" "$(third | sed 's/^id: third-row$/id: off-outcome/; s/^  outcome: .*/  outcome: pending/')" 'upstream.outcome "pending"'
case_ "a missing key" 1 "no-verified.md" "$(third | sed 's/^id: third-row$/id: no-verified/; /^verified:/d')" "verified is missing"
case_ "a bad date" 1 "bad-date.md" "$(third | sed 's/^id: third-row$/id: bad-date/; s/^verified: .*/verified: yesterday/')" 'verified "yesterday"'
case_ "a bare reference" 1 "bare-ref.md" "$(third | sed 's/^id: third-row$/id: bare-ref/; s/^  refs: .*/  refs: [#12]/')" 'upstream.refs "#12"'
case_ "the internal tracker" 1 "tracker.md" "$(third | sed 's/^id: third-row$/id: tracker/; s/^  refs: .*/  refs: [giantswarm\/giantswarm#37742]/')" "internal tracker"
case_ "an exit missing while open" 1 "no-exit.md" "$(third | sed 's/^id: third-row$/id: no-exit/; /^  exit:/d')" "upstream.exit is empty while outcome is open"
case_ "an exit missing while merged" 0 "merged.md" "$(third | sed 's/^id: third-row$/id: merged/; s/^  outcome: .*/  outcome: merged/; /^  exit:/d')"
case_ "a dependency on no file" 1 "no-dep.md" "$(third | sed 's/^id: third-row$/id: no-dep/; s/^depends_on: .*/depends_on: [missing-row]/')" 'depends_on "missing-row" names no file'
case_ "a malformed dependency" 1 "bad-dep.md" "$(third | sed 's/^id: third-row$/id: bad-dep/; s/^depends_on: .*/depends_on: [Not A Row]/')" 'depends_on "Not A Row"'
case_ "no commits while carried" 1 "no-commits.md" "$(third | sed 's/^id: third-row$/id: no-commits/; /^  - .fix(x)/d')" "commits is empty while status is carried"
case_ "no commits while dropped" 0 "dropped.md" "$(third | sed 's/^id: third-row$/id: dropped/; s/^status: .*/status: dropped/; /^  - .fix(x)/d')"
case_ "commits as a flow list" 0 "flow.md" "$(third | sed 's/^id: third-row$/id: flow/; /^  - .fix(x)/d; s/^commits:$/commits: ["fix(x): a flow subject"]/')"
case_ "commits missing" 1 "no-commits-key.md" "$(third | sed 's/^id: third-row$/id: no-commits-key/; /^commits:$/d; /^  - .fix(x)/d')" "commits is missing"
case_ "a pull request suffix on a subject" 1 "suffix.md" "$(third | sed 's/^id: third-row$/id: suffix/; s/^  - .fix(x).*/  - "fix(x): a subject (#12)"/')" "keeps its pull request suffix"
case_ "the retired commit key" 1 "old-commit.md" "$(third | sed 's/^id: third-row$/id: old-commit/; s/^verified:/commit: 52f1c49a\nverified:/')" "commit is no longer a field"
case_ "a subject named twice" 1 "twice.md" "$(third | sed "s/^id: third-row$/id: twice/; s/^  - 'fix(x).*/  - 'fix(ateom): arm the tunnel first'/")" "is named by another-row twice"
case_ "no History section" 1 "no-history.md" "$(third | sed 's/^id: third-row$/id: no-history/; /^## History/,$d')" "no ## History section"
case_ "a Status bullet" 1 "bullet.md" "$(third | sed 's/^id: third-row$/id: bullet/; s/^# A good row$/# A good row\n\n- **Status:** carried/')" "repeats the header"
case_ "a carried-commit table" 1 "table.md" "$(third | sed 's/^id: third-row$/id: table/; s/^# A good row$/# A good row\n\n| Carried commit (subject) | Why | Upstream |\n|---|---|---|\n| `x` | y | z |/')" "table repeats the header"
case_ "a subject the YAML quoting ends early" 1 "bad-quote.md" "$(third | sed "s/^id: third-row\$/id: bad-quote/; s/^  - 'fix(x).*/  - 'fix(x): the line's subject'/")" "not valid YAML"

# complete_ <name> <want exit code> <ledger text for a-change.md> <EXEMPT text> [text the output must name]
complete_() {
  local name="$1" want="$2" text="$3" exempt="$4" names="${5:-}" got=0 out repo
  repo="${DIR}/repo-${name// /-}"
  git init -q -b main "${repo}"
  git -C "${repo}" -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false commit -q --allow-empty -m "upstream: the pin"
  git -C "${repo}" checkout -q -b giantswarm
  git -C "${repo}" -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false commit -q --allow-empty -m "fix(x): a carried change (#7)"
  git -C "${repo}" -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false commit -q --allow-empty -m "docs(fork): its ledger file"
  git -C "${repo}" -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false commit -q --allow-empty -m "chore(deps): a bot's bump"
  mkdir -p "${repo}/fork"
  printf '%s\n' "${text}" >"${repo}/fork/a-change.md"
  [[ -n "${exempt}" ]] && printf '%s\n' "${exempt}" >"${repo}/fork/EXEMPT"
  out="$(cd "${repo}" && "${CHECK}" substrate fork --complete main 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]] || [[ -n "${names}" && "${out}" != *"${names}"* ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}${names:+ naming ${names}}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

named() { good | sed "s/^id: a-good-row$/id: a-change/; s/^  - 'fix(atenet).*/  - 'fix(x): a carried change'/; s/^depends_on: .*/depends_on: []/"; }
exempt=$'# comment\ndocs(\nchore(deps)'

complete_ "every commit named or exempt" 0 "$(named)" "${exempt}"
complete_ "a commit no file names" 1 "$(named | sed "s/^  - 'fix(x).*/  - 'fix(y): something else'/")" "${exempt}" '"fix(x): a carried change" is named by 0 file(s)'
complete_ "an exemption missing" 1 "$(named)" $'docs('  '"chore(deps): a bot'"'"'s bump" is named by 0 file(s)'
complete_ "no EXEMPT file" 1 "$(named)" "" "no EXEMPT file"

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
