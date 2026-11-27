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

# Fork (giantswarm/substrate): fixture runs of fork-closing-keywords.sh.

set -o errexit -o nounset -o pipefail

LINT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/fork-closing-keywords.sh"

failures=0

# case <name> <want exit code> <text>
case_() {
  local name="$1" want="$2" text="$3" got=0
  "${LINT}" <<<"${text}" >/dev/null 2>&1 || got=$?
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}"
    failures=$((failures + 1))
  fi
}

case_ "a quoted upstream keyword" 1 'agent-substrate/substrate#2706 (ericcurtin, "Fixes #2555") added as the ref'
case_ "a cross-repository reference" 1 'Closes agent-substrate/substrate#2555'
case_ "an issue URL" 1 'resolved https://github.com/agent-substrate/substrate/issues/2555'
case_ "a colon after the keyword" 1 'Fix: #12'
case_ "any case" 1 'RESOLVES #3'
case_ "a reference without a keyword" 0 'agent-substrate/substrate#2706, for #2555'
case_ "a keyword without a reference" 0 'fix(sync): name the reason when no upstream pin resolves'
case_ "a keyword inside a word" 0 'prefixes #4 and suffixes #5'
case_ "a fixture name" 0 'fixtures #1 to #3'
case_ "empty text" 0 ''

if ((failures)); then
  exit 1
fi
echo "fork-closing-keywords: every fixture passed"
