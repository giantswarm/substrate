// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dns

import (
	"github.com/agent-substrate/substrate/internal/egresspolicy"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"strings"
)

// evaluator decides whether a DNS hostname is allowed by an EgressPolicy's
// rules.
type evaluator struct {
	patterns []egresspolicy.HostnamePattern
}

// newEvaluator compiles the hostname patterns allowed by policy's rules. A nil
// policy, or one with no matching rules, allows nothing. Invalid patterns are
// ignored so they fail closed without breaking valid patterns alongside them.
func newEvaluator(policy *ateletpb.EgressPolicy) *evaluator {
	e := &evaluator{}
	var seen map[string]struct{}
	for _, rule := range policy.GetRules() {
		var dest *ateletpb.EgressDestinations
		switch {
		case rule.GetHttp() != nil:
			dest = rule.GetHttp()
		case rule.GetHttps() != nil:
			dest = rule.GetHttps()
		case rule.GetTcp() != nil:
			dest = rule.GetTcp()
		case rule.GetTlsPassthrough() != nil:
			dest = rule.GetTlsPassthrough()
		default:
			continue
		}
		for _, raw := range dest.GetHostnames() {
			if _, ok := seen[raw]; ok {
				continue
			}
			pattern, err := egresspolicy.ParseHostnamePattern(raw)
			if err != nil {
				continue
			}
			if seen == nil {
				seen = make(map[string]struct{})
			}
			seen[raw] = struct{}{}
			e.patterns = append(e.patterns, pattern)
		}
	}
	return e
}

// allowsHostname reports whether qname is a valid DNS hostname matched by at
// least one rule in the policy.
func (e *evaluator) allowsHostname(qname string, searchDomains ...string) bool {
	if e == nil || len(e.patterns) == 0 {
		return false
	}
	host, err := egresspolicy.ParseExactHostname(strings.ToLower(strings.TrimSuffix(qname, ".")))
	if err != nil {
		return false
	}
	for _, p := range e.patterns {
		if p.Matches(host) {
			return true
		}
		// Kubernetes expands short service names through its cluster search
		// list. Only trusted suffixes from the worker's ClusterFirst config
		// reach this list; node-supplied external search domains do not.
		for _, suffix := range searchDomains {
			if short, ok := strings.CutSuffix(host, "."+suffix); ok && short != "" && p.Matches(short) {
				return true
			}
		}
	}
	return false
}
