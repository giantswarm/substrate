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

# Download atomically so an interrupted request never becomes a cache hit.
set -o errexit -o nounset -o pipefail
url="$1"
destination="$2"
if [[ -s "$destination" ]]; then
  echo "Cached: $destination" >&2
  exit 0
fi
mkdir -p "$(dirname "$destination")"
partial=$(mktemp "${destination}.XXXXXX")
trap 'rm -f "$partial"' EXIT
curl --fail --location --silent --show-error --retry 5 --retry-max-time 180 \
  --connect-timeout 15 --max-time 60 --output "$partial" "$url"
mv "$partial" "$destination"
