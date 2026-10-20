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

set -o nounset -o pipefail

# Writes what a failed e2e run needs for diagnosis into DIR: the failed tests
# with the time each failed, the pods, every event, each restarted container's
# end, and the whole log of every control-plane pod and every worker pod.
#
# Usage: hack/dump-e2e-diagnostics.sh DIR
#
# A tail covers only the last seconds of a busy control plane, long after the
# test failed, so each log is written whole; the inline output stays a first
# look: the failed tests, the pods, and the end of each log. Worker pods are
# read in any namespace: the demo pools plus the suites' per-test namespaces,
# which the suites keep on failure. The failing actor ran in one of these, so
# that is where its ateom logs (and, for a micro-VM worker, the guest console
# tail) are.
#
# ARTIFACTS names the directory of the lanes' go test event streams (*.json).
# KUBE_CONTEXT selects the cluster (default kind-kind). Every read is
# best-effort: a diagnostics dump never fails the step that runs it.

dir="${1:?usage: $0 DIR}"
context="${KUBE_CONTEXT:-kind-kind}"
mkdir -p "${dir}"

k() { kubectl --context "${context}" "$@"; }

echo "=== failed tests (from each lane's go test event stream) ==="
for f in "${ARTIFACTS:-.}"/*.json; do
  [ -f "$f" ] || continue
  jq -r --arg lane "$(basename "$f" .json)" \
    'select(.Action == "fail" and .Test) | "\(.Time) \($lane) \(.Test)"' "$f" 2>/dev/null || true
done | tee "${dir}/failed-tests.txt"

k get workerpool,pods -A -o wide 2>&1 | tee "${dir}/pods.txt"
k get events -A --sort-by=.lastTimestamp > "${dir}/events.txt" 2>&1

# A restarted container's own end: its termination reason (OOMKilled, Error,
# the exit code) and when it ended; its log is the .previous.log below.
# shellcheck disable=SC2016 # a Go template, not a shell expansion
ended="$(printf '%s' \
  '{{range .items}}{{$p := .}}{{range $c := .status.containerStatuses}}{{with $c.lastState.terminated}}' \
  '{{$p.metadata.namespace}}/{{$p.metadata.name}} {{$c.name}}: restarts={{$c.restartCount}}' \
  ' reason={{.reason}} exitCode={{.exitCode}}' \
  ' finishedAt={{.finishedAt}}{{"\n"}}{{end}}{{end}}{{end}}')"
echo "=== restarted containers ==="
k get pods -A -o go-template="${ended}" 2>&1 | tee "${dir}/restarted-containers.txt"

dump() {
  local log="${dir}/$1/$2.log"
  mkdir -p "${dir}/$1"
  k logs -n "$1" "$2" --all-containers --timestamps > "${log}" 2>&1
  # A restarted container's earlier run is often where the failure is.
  k logs -n "$1" "$2" --all-containers --timestamps --previous \
    > "${log%.log}.previous.log" 2>/dev/null || rm -f "${log%.log}.previous.log"
  echo "=== logs: $1/$2 (last 50 lines) ==="
  tail -n 50 "${log}"
}
pods() { k get pods "$@" -o 'custom-columns=:.metadata.namespace,:.metadata.name' --no-headers 2>/dev/null; }

{ pods -n ate-system; pods -A -l ate.dev/worker-pool; } \
  | while read -r ns name; do dump "$ns" "$name"; done
exit 0
