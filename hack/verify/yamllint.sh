#!/usr/bin/env bash

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Lints the repository's own workflow files with .yamllint.yaml. Left out:
# the zz_generated.* workflows, which only giantswarm/devctl changes, and the
# files in SKIP, which carry known errors that wait on a pending change.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

SKIP=(pr-workflow.yaml helm-e2e.yaml)

FILES=()
for file in .github/workflows/*.yaml; do
  name="$(basename "${file}")"
  case "${name}" in
    zz_generated.*) continue ;;
  esac
  if printf '%s\n' "${SKIP[@]}" | grep -qxF "${name}"; then
    continue
  fi
  FILES+=("${file}")
done

./hack/run-tool.sh yamllint -c .yamllint.yaml "${FILES[@]}"
