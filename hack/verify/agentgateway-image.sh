#!/usr/bin/env bash

# Copyright 2026 The Agent Substrate Authors
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

# Fork (giantswarm/substrate): every install of the line runs the agentgateway
# build the chart pins. The e2e installs from manifests/ate-install, so a
# kustomize image left on another build tests a data plane the line does not
# ship.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

VALUES="charts/substrate/values.yaml"
CIRCLECI=".circleci/config.yml"
KUSTOMIZATION="manifests/ate-install/components/agentgateway/kustomization.yaml"

pinned="$(sed -n 's/^  agentgateway: //p' "${VALUES}")"
if [[ -z "${pinned}" ]]; then
  echo "${VALUES} has no images.agentgateway." >&2
  exit 1
fi

failed=0
circleci="$(sed -n '/^  agentgateway-image:$/,/default:/s/^ *default: //p' "${CIRCLECI}")"
if [[ "${circleci}" != "${pinned}" ]]; then
  echo "${CIRCLECI} agentgateway-image is ${circleci:-unset}, the chart pins ${pinned}." >&2
  failed=1
fi

images=()
while IFS= read -r image; do
  images+=("${image}")
done < <(sed -n 's/^ *image: //p' "${KUSTOMIZATION}")
if (( ${#images[@]} == 0 )); then
  echo "${KUSTOMIZATION} names no image." >&2
  failed=1
fi
for image in "${images[@]}"; do
  if [[ "${image%@sha256:*}" != "${pinned}" ]]; then
    echo "${KUSTOMIZATION} runs ${image}, the chart pins ${pinned}." >&2
    failed=1
  fi
done

exit "${failed}"
