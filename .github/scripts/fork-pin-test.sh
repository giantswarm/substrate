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

# Fork (giantswarm/substrate): fixture runs of fork-pin.sh.

set -o errexit -o nounset -o pipefail

PIN="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/fork-pin.sh"

dir="$(mktemp -d)"
trap 'rm -rf "${dir}"' EXIT
failures=0

g() { git -C "${dir}" -c user.name=pin-test -c user.email=pin-test@example.invalid -c commit.gpgsign=false -c tag.gpgsign=false "$@"; }

# case <name> <want exit code> <want output> [<rev>]
case_() {
  local name="$1" want="$2" out="$3" rev="${4:-HEAD}" got=0 printed
  printed="$(cd "${dir}" && "${PIN}" "${rev}" 2>/dev/null)" || got=$?
  if [[ "${got}" != "${want}" || "${printed}" != "${out}" ]]; then
    echo "FAIL ${name}: exit ${got} printing '${printed}', want ${want} printing '${out}'"
    failures=$((failures + 1))
  else
    echo "ok   ${name}"
  fi
}

g init -q -b main
g commit -q --allow-empty -m upstream-1
case_ "no upstream tag reachable" 1 ""
g tag upstream/v1.0.0-rc4
g tag -a upstream/v1.0.0 -m v1.0.0
g commit -q --allow-empty -m upstream-2
g tag -a upstream/v1.0.1 -m v1.0.1
g commit -q --allow-empty -m upstream-3
g checkout -q -b line upstream/v1.0.1
g commit -q --allow-empty -m carried-1
g tag v1.0.1
g commit -q --allow-empty -m carried-2
g tag v2.0.0
case_ "the nearest upstream tag, not the line's own v* tags" 0 v1.0.1
case_ "a stable tag over a prerelease on the same commit" 0 v1.0.0 upstream/v1.0.0
case_ "an upstream commit past every tag is not reachable from the line" 0 v1.0.1 line
g tag upstream/v1.1.0-alpha1 main
case_ "a later upstream tag off the line does not count" 0 v1.0.1

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
