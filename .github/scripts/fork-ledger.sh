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

# Fork (giantswarm/substrate): lints the ledger in FORK.md (the workflow
# fork-ledger runs it on every pull request against a line).
#
#   fork-ledger.sh pr   <pull-request.json> <FORK.md>
#       the pull request names its own number in the ledger as #<n>;
#   fork-ledger.sh line <merged-pull-requests.json> <FORK.md>
#       every pull request merged into the line does (a maintenance branch:
#       its own copy of FORK.md is its ledger).
#
# The JSON is what `gh pr view --json` / `gh pr list --json` print with the
# fields number, title, body, labels, files, headRefName and author. A pull
# request needs no row when it changes FORK.md alone, its title is
# `docs(fork)…`, it comes from a re-pin candidate (`sync/…`, closed by the
# landing, never merged), a bot opened it, or it carries the label
# `no-ledger` with a line `no-ledger: <reason>` in its description.
# Exit 1 names every missing number.

set -o errexit -o nounset -o pipefail

usage() {
  echo "usage: $0 pr|line <json> <FORK.md>" >&2
  exit 2
}

# The pull requests of the JSON (one object or an array) that need a row and
# have none, one per line as "<number>\t<reason the exemption failed>".
missing() {
  local json="$1" ledger="$2"
  local n why
  while IFS=$'\t' read -r n why; do
    if ! grep -qE "(^|[^[:alnum:]/_.-])#${n}([^0-9]|$)|/pull/${n}([^0-9]|$)" "${ledger}"; then
      printf '%s\t%s\n' "${n}" "${why}"
    fi
  done < <(jq -r '
    if type == "array" then .[] else . end
    | select((.headRefName // "") | startswith("sync/") | not)
    | select(.author.is_bot // false | not)
    | select((.title // "") | test("^docs\\(fork\\)") | not)
    | select([.files[]?.path] | any(. != "FORK.md"))
    | (([.labels[]?.name] | index("no-ledger")) != null) as $labelled
    | ((.body // "") | test("(?m)^\\s*no-ledger:\\s*\\S")) as $reason
    | select(($labelled and $reason) | not)
    | [.number,
       (if $labelled then "labelled no-ledger without a \"no-ledger: <reason>\" line in the description"
        else "no row names it" end)]
    | @tsv' "${json}")
}

[[ $# -eq 3 ]] || usage
mode="$1" json="$2" ledger="$3"
[[ -f "${json}" && -f "${ledger}" ]] || usage

case "${mode}" in
pr | line) ;;
*) usage ;;
esac

result="$(missing "${json}" "${ledger}")"
if [[ -z "${result}" ]]; then
  echo "fork-ledger: every pull request checked has its row in ${ledger}"
  exit 0
fi

while IFS=$'\t' read -r n why; do
  echo "::error file=FORK.md::#${n} is missing from the ledger (${why}): add a row naming #${n}, or label the pull request no-ledger with a line \"no-ledger: <reason>\" in its description"
done <<<"${result}"
echo "fork-ledger: missing $(cut -f1 <<<"${result}" | sed 's/^/#/' | paste -sd' ')" >&2
exit 1
