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

# Fork (giantswarm/substrate): a pull request closes no issue by keyword.
# GitHub honours "Fixes #<n>" and its kin in a pull request's description and
# in the commits it lands, upstream issues included: a ledger description
# quoting an upstream pull request's "Fixes #2555" closed
# agent-substrate/substrate#2555 when it merged. The workflow fork-ledger pipes
# in the title, the description and every commit message.
#
#   fork-closing-keywords.sh <text
#
# A match is a closing keyword (close, closes, closed, fix, fixes, fixed,
# resolve, resolves, resolved; any case, an optional colon) followed by an
# issue reference: #<n>, <owner>/<repo>#<n> or an issues or pull URL. Quote
# such a phrase without its keyword, or name the reference on its own.

set -o errexit -o nounset -o pipefail

pattern='(^|[^[:alnum:]_])(close[sd]?|fix(e[sd])?|resolve[sd]?)[[:space:]]*:?[[:space:]]+(([[:alnum:]_.-]+/[[:alnum:]_.-]+)?#[0-9]+|https://github[.]com/[^[:space:]/]+/[^[:space:]/]+/(issues|pull)/[0-9]+)'

if matches="$(grep -Ein -- "${pattern}")"; then
  while IFS= read -r line; do
    echo "::error::closing keyword with an issue reference (line ${line%%:*}): ${line#*:}"
  done <<<"${matches}"
  echo "a closing keyword closes the referenced issue, upstream included: drop the keyword (\"#2706 fixes #2555\" becomes \"#2706, for #2555\")" >&2
  exit 1
fi
