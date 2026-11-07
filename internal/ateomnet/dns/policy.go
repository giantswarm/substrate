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
	"context"
	"log/slog"
	"sync"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
)

// PolicySource supplies the egress policy an actor's DNS queries are checked
// against.
type PolicySource interface {
	// EgressPolicy returns the actor's current policy.
	//
	// Callers must not modify the policy.
	EgressPolicy(ctx context.Context) (*ateletpb.EgressPolicy, error)
}

type queryPolicy func(dnsmessage.Question) bool

// egressPolicyCheck returns the queryPolicy that checks each DNS question against
// the actor's egress policy.
func egressPolicyCheck(ctx context.Context, source PolicySource, searchDomains ...string) queryPolicy {
	if source == nil {
		return func(dnsmessage.Question) bool { return false }
	}
	var (
		mu     sync.Mutex
		cached *ateletpb.EgressPolicy
		eval   *evaluator
	)
	return func(q dnsmessage.Question) bool {
		policy, err := source.EgressPolicy(ctx)
		if err != nil {
			slog.DebugContext(ctx, "dns relay refused a query; the actor's egress policy is unavailable", slog.Any("err", err))
			return false
		}
		mu.Lock()
		if policy != cached || eval == nil {
			cached = policy
			eval = newEvaluator(policy)
		}
		e := eval
		mu.Unlock()
		return e.allowsHostname(q.Name.String(), searchDomains...)
	}
}

// checkQuery refuses malformed messages and disallowed questions before any
// upstream connection. Checking every question prevents multi-question bypasses.
func checkQuery(ctx context.Context, source PolicySource, query []byte, searchDomains ...string) []byte {
	var msg dnsmessage.Message
	if err := msg.Unpack(query); err != nil {
		return []byte{}
	}
	allowed := !msg.Header.Response && msg.Header.OpCode == 0 && len(msg.Questions) > 0
	check := egressPolicyCheck(ctx, source, searchDomains...)
	for _, q := range msg.Questions {
		if q.Class != dnsmessage.ClassINET || !check(q) {
			allowed = false
		}
	}
	if allowed {
		return nil
	}
	msg.Header.Response = true
	msg.Header.RCode = dnsmessage.RCodeNameError
	msg.Header.RecursionAvailable = false
	msg.Answers, msg.Authorities, msg.Additionals = nil, nil, nil
	answer, err := msg.Pack()
	if err != nil {
		return []byte{}
	}
	return answer
}
