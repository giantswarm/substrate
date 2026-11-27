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

# Fork (giantswarm/substrate): fixture runs of release-tag.sh, each case a
# throwaway repository whose commit carries the given tags.

set -o errexit -o nounset -o pipefail

CHECK="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-tag.sh"
DIR="$(mktemp -d)"
trap 'rm -rf "${DIR}"' EXIT

# The fixtures ignore the caller's git configuration (signing, hooks).
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=fixture@example.com
export GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=fixture@example.com

failures=0

# case_ <name> <want exit code> <checked tag> <tags on the commit (a:… annotated)> [<tag on a second commit>]
case_() {
  local name="$1" want="$2" checked="$3" tags="$4" elsewhere="${5:-}" repo got=0 out t
  repo="$(mktemp -d "${DIR}/repo.XXXXXX")"
  git -C "${repo}" init --quiet
  git -C "${repo}" commit --quiet --allow-empty -m one
  for t in ${tags}; do
    if [[ "${t}" == a:* ]]; then
      git -C "${repo}" tag -a -m "${t#a:}" "${t#a:}"
    else
      git -C "${repo}" tag "${t}"
    fi
  done
  if [[ -n "${elsewhere}" ]]; then
    git -C "${repo}" commit --quiet --allow-empty -m two
    git -C "${repo}" tag "${elsewhere}"
  fi
  out="$(cd "${repo}" && "${CHECK}" "${checked}" 2>&1)" || got=$?
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL ${name}: exit ${got}, want ${want}"
    printf '%s\n' "${out}" | sed 's/^/    /'
    failures=$((failures + 1))
    return
  fi
  # A refusal names the checked tag and every conflicting one.
  if [[ "${want}" == 1 ]]; then
    for t in ${tags}; do
      t="${t#a:}"
      [[ "${t%%-rc.*}" == "${checked%%-rc.*}" || ! "${t}" =~ ^v[0-9] ]] && continue
      if [[ "${out}" != *"${checked}"* || "${out}" != *"${t}"* ]]; then
        echo "FAIL ${name}: the refusal does not name ${checked} and ${t}"
        printf '%s\n' "${out}" | sed 's/^/    /'
        failures=$((failures + 1))
        return
      fi
    done
  fi
  echo "ok   ${name}"
}

case_ "one release" 0 v1.2.0 "v1.2.0"
case_ "a candidate alone" 0 v1.2.0-rc.1 "v1.2.0-rc.1"
case_ "a stable on its candidate" 0 v1.2.0 "v1.2.0-rc.3 v1.2.0"
case_ "two candidates of one version" 0 v1.2.0-rc.2 "v1.2.0-rc.1 v1.2.0-rc.2"
case_ "an upstream prerelease on the commit" 0 v1.2.0 "v0.5.0-alpha2 v1.2.0"
case_ "the other version on another commit" 0 v1.2.0 "v1.2.0" v1.1.4
case_ "a minor on a patch's commit" 1 v1.2.0 "v1.1.4 v1.2.0"
case_ "a patch on a minor's commit" 1 v1.1.4 "v1.1.4 v1.2.0"
case_ "a candidate on another release's commit" 1 v1.2.0-rc.1 "v1.1.4 v1.2.0-rc.1"
case_ "annotated tags" 1 v1.2.0 "a:v1.1.4 a:v1.2.0"
case_ "two other versions" 1 v1.3.0 "v1.1.4 v1.2.0-rc.1 v1.3.0"
case_ "not a release tag" 2 v0.5.0-alpha2 "v0.5.0-alpha2"
case_ "no such tag" 2 v9.9.9 "v1.2.0"

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures} case(s) failed"
  exit 1
fi
