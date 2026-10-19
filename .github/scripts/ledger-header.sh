#!/bin/sh

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

# Fork (giantswarm/substrate): every carried-change file under the ledger
# directory opens with the YAML front-matter `fork-ledger validate` reads
# (FORK.md, the ledger; giantswarm/kagent-upstream#719). This check is the
# same rules in POSIX sh and awk, so CI needs no Go module to run it.
#
#   .github/scripts/ledger-header.sh <fork> <dir>   # one line per problem on stderr, exit 1 on any
#
# A header is the block between the opening `---` on line 1 and the next
# `---`: `id` is the file name without .md, `fork` is <fork>, `theme` is one
# of the twenty shared slugs, `category`, `status` and `upstream.outcome` take
# their listed values, `upstream.refs` are `owner/repo#N` and never name
# giantswarm/giantswarm, `depends_on` entries are such a reference, a file of
# this ledger, or `owner/repo:<id>` in another line's ledger, `verified` is a
# date, `upstream.exit` is set unless the outcome is merged or none, and
# `commit` is empty, a 7-12 hex SHA or `owner/repo#N`.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <fork> <dir>" >&2
  exit 64
fi
fork=$1
dir=$2
if [ ! -d "$dir" ]; then
  echo "$dir: not a directory" >&2
  exit 64
fi

ids=$(for f in "$dir"/*.md; do b=${f##*/}; [ "$b" = README.md ] && continue; printf '%s\n' "${b%.md}"; done)

problems=0
for f in "$dir"/*.md; do
  base=${f##*/}
  [ "$base" = README.md ] && continue
  if ! out=$(awk -v fork="$fork" -v id="${base%.md}" -v ids="$ids" '
    function unquote(v,    q) {
      sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v)
      q = substr(v, 1, 1)
      if ((q == "\"" || q == "'"'"'") && substr(v, length(v), 1) == q && length(v) >= 2) {
        v = substr(v, 2, length(v) - 2)
        if (q == "'"'"'") gsub(/'"''"'/, "'"'"'", v)
      }
      return v
    }
    function problem(m) { print m; problems++ }
    function islist(v) { return v ~ /^\[.*\]$/ }
    function items(v, arr,    n, i, raw) {
      v = substr(v, 2, length(v) - 2)
      if (v ~ /^[ \t]*$/) return 0
      n = split(v, raw, /,[ \t]*/)
      for (i = 1; i <= n; i++) arr[i] = unquote(raw[i])
      return n
    }
    BEGIN {
      split("session-lifecycle workspace-volumes credential-injection durable-pause egress-policies providers-vertex harness-variety golden-boot identity-authorizer mcp-forwarder jwks tenancy compat-migrations chart-knobs chart-shape ci-publish-infra test-flakes version-pins new-rpcs never-filed", t, " ")
      for (i in t) themes[t[i]] = 1
      split("pin infra bugfix feature gs-specific compat test", c, " "); for (i in c) categories[c[i]] = 1
      split("carried dropped superseded", s, " "); for (i in s) statuses[s[i]] = 1
      split("merged open declined superseded to-file fork-only drops-at-repin none", o, " "); for (i in o) outcomes[o[i]] = 1
      n = split(ids, idlist, "\n"); for (i = 1; i <= n; i++) known[idlist[i]] = 1
      problems = 0; closed = 0; in_upstream = 0
    }
    NR == 1 { if ($0 != "---") { problem("no header: the file must start with a --- front-matter"); exit } next }
    closed { next }
    $0 == "---" { closed = 1; next }
    /^[a-z_]+:/ {
      in_upstream = 0
      key = $0; sub(/:.*/, "", key)
      val = $0; sub(/^[a-z_]+:/, "", val)
      if (key == "upstream") { in_upstream = 1; seen["upstream"] = 1; next }
      seen[key] = 1; top[key] = val; next
    }
    in_upstream && /^  [a-z_]+:/ {
      key = $0; sub(/^  /, "", key); sub(/:.*/, "", key)
      val = $0; sub(/^  [a-z_]+:/, "", val)
      seen["upstream." key] = 1; up[key] = val; next
    }
    { problem("header: unexpected line " NR ": " $0) }
    END {
      if (NR == 0) { problem("no header: the file is empty"); print "PROBLEMS=" problems; exit }
      if (problems && !closed && NR == 1) { print "PROBLEMS=" problems; exit }
      if (!closed) problem("no header: the front-matter never closes with ---")
      split("id fork theme category status upstream.outcome verified", req, " ")
      for (i in req) if (!(req[i] in seen)) problem("header: " req[i] " is missing")
      v = unquote(top["id"]); if (("id" in seen) && v != id) problem("id \"" v "\" does not match the file name \"" id "\"")
      v = unquote(top["fork"]); if (("fork" in seen) && v != fork) problem("fork \"" v "\" is not \"" fork "\"")
      v = unquote(top["theme"]); if (("theme" in seen) && !(v in themes)) problem("theme \"" v "\" is not in the catalogue")
      v = unquote(top["category"]); if (("category" in seen) && !(v in categories)) problem("category \"" v "\" is not pin, infra, bugfix, feature, gs-specific, compat or test")
      v = unquote(top["status"]); if (("status" in seen) && !(v in statuses)) problem("status \"" v "\" is not carried, dropped or superseded")
      outcome = unquote(up["outcome"]); if (("upstream.outcome" in seen) && !(outcome in outcomes)) problem("upstream.outcome \"" outcome "\" is not merged, open, declined, superseded, to-file, fork-only, drops-at-repin or none")
      v = unquote(top["verified"]); if (("verified" in seen) && v !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/) problem("verified \"" v "\" is not a YYYY-MM-DD date")
      if ("upstream.refs" in seen) {
        v = unquote(up["refs"])
        if (!islist(v)) problem("upstream.refs is not a [list]")
        else { n = items(v, refs); for (i = 1; i <= n; i++) {
          if (refs[i] !~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) problem("upstream.refs \"" refs[i] "\" is not owner/repo#N")
          else if (refs[i] ~ /^giantswarm\/giantswarm#/) problem("upstream.refs \"" refs[i] "\" names the internal tracker")
        } }
      }
      exitv = unquote(up["exit"])
      if ((exitv == "" || exitv == "-") && outcome != "merged" && outcome != "none") problem("upstream.exit is empty while outcome is " outcome)
      if ("depends_on" in seen) {
        v = unquote(top["depends_on"])
        if (!islist(v)) problem("depends_on is not a [list]")
        else { n = items(v, deps); for (i = 1; i <= n; i++) {
          d = deps[i]
          if (d ~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) { if (d ~ /^giantswarm\/giantswarm#/) problem("depends_on \"" d "\" names the internal tracker"); continue }
          if (d ~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+:[a-z0-9][a-z0-9-]*$/) continue
          if (d ~ /^[a-z0-9][a-z0-9-]*$/) { if (!(d in known)) problem("depends_on \"" d "\" names no file in this ledger"); continue }
          problem("depends_on \"" d "\" is neither owner/repo#N nor [owner/repo:]id")
        } }
      }
      v = unquote(top["commit"])
      if (v != "" && v !~ /^[0-9a-f]+$/ && v !~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) problem("commit \"" v "\" is neither a SHA nor owner/repo#N")
      else if (v ~ /^[0-9a-f]+$/ && (length(v) < 7 || length(v) > 12)) problem("commit \"" v "\" is not a 7-12 hex SHA")
      print "PROBLEMS=" problems
    }
  ' "$f") || true; then :; fi
  count=$(printf '%s\n' "$out" | sed -n 's/^PROBLEMS=//p' | tail -1)
  printf '%s\n' "$out" | grep -v '^PROBLEMS=' | sed "s|^|$f: |" >&2 || true
  [ -z "$count" ] && count=1
  problems=$((problems + count))
done

if [ "$problems" -gt 0 ]; then
  echo "$problems problem(s) in $dir" >&2
  exit 1
fi
