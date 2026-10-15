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

# Fork (giantswarm/substrate): fixture runs of fork-public-links.sh against a
# stand-in gh that knows two public repositories.

set -o errexit -o nounset -o pipefail

LINT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/fork-public-links.sh"
DIR="$(mktemp -d)"
trap 'rm -rf "${DIR}"' EXIT

cat >"${DIR}/gh" <<'GH'
#!/usr/bin/env bash
case "$2" in
repos/giantswarm/substrate | repos/giantswarm/agent-platform) echo public ;;
*) echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
esac
GH
chmod +x "${DIR}/gh"

failures=0

# case <name> <want exit code> <ledger text> [text the output must name]
case_() {
  local name="$1" want="$2" text="$3" names="${4:-}" got=0 out
  out="$(PATH="${DIR}:${PATH}" "${LINT}" <<<"${text}" 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]] || [[ -n "${names}" && "${out}" != *"${names}"* ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}${names:+ naming ${names}}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

case_ "no citation" 0 "- **Upstream:** to file — prepared in the fork: branch \`upstream/x\`"
case_ "public links" 0 "[#7](https://github.com/giantswarm/substrate/pull/7), giantswarm/agent-platform#12"
case_ "an image path is no citation" 0 "\`gsoci.azurecr.io/giantswarm/pause:3.10.2\`"
case_ "the internal repository" 1 "queued on [giantswarm/giantswarm#1](https://github.com/giantswarm/giantswarm/issues/1)" "giantswarm/giantswarm (internal)"
case_ "a private repository as #n" 1 "see giantswarm/secret-repo#3" "giantswarm/secret-repo (not found)"
case_ "a private repository's file" 1 "https://github.com/giantswarm/secret-repo/blob/main/x.md." "giantswarm/secret-repo (not found)"
case_ "names every line" 1 "$(printf '%s\n' 'giantswarm/substrate#1' 'giantswarm/a#2' 'giantswarm/b#3')" "2 citation(s)"
case_ "empty input" 0 ""

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
