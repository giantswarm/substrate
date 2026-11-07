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
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"golang.org/x/net/dns/dnsmessage"
)

type staticPolicy struct {
	policy *ateletpb.EgressPolicy
	err    error
}

func (p staticPolicy) EgressPolicy(context.Context) (*ateletpb.EgressPolicy, error) {
	return p.policy, p.err
}

var withPolicy = staticPolicy{policy: &ateletpb.EgressPolicy{Rules: []*ateletpb.EgressRule{{Http: &ateletpb.EgressDestinations{Hostnames: []string{"*"}}}}}}

func policyFor(host string) staticPolicy {
	return staticPolicy{policy: &ateletpb.EgressPolicy{Rules: []*ateletpb.EgressRule{{Https: &ateletpb.EgressDestinations{Hostnames: []string{host}}}}}}
}
func queryFor(t *testing.T, host string, typ dnsmessage.Type) []byte {
	t.Helper()
	name, err := dnsmessage.NewName(host)
	if err != nil {
		t.Fatal(err)
	}
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: 17, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: typ, Class: dnsmessage.ClassINET}}}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestQueryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  PolicySource
		host    string
		typ     dnsmessage.Type
		allowed bool
	}{
		{"exact", policyFor("example.com"), "example.com.", dnsmessage.TypeA, true},
		{"AAAA", policyFor("example.com"), "EXAMPLE.COM.", dnsmessage.TypeAAAA, true},
		{"TXT allowed", policyFor("example.com"), "example.com.", dnsmessage.TypeTXT, true},
		{"wildcard", policyFor("*.example.com"), "a.example.com.", dnsmessage.TypeA, true},
		{"outside", policyFor("example.com"), "blocked.test.", dnsmessage.TypeA, false},
		{"tunnel", policyFor("example.com"), "secret.example.com.", dnsmessage.TypeTXT, false},
		{"suffix spoof", policyFor("*.example.com"), "example.com.evil.test.", dnsmessage.TypeA, false},
		{"nil source", nil, "example.com.", dnsmessage.TypeA, false},
		{"nil policy", staticPolicy{}, "example.com.", dnsmessage.TypeA, false},
		{"empty", staticPolicy{policy: &ateletpb.EgressPolicy{}}, "example.com.", dnsmessage.TypeA, false},
		{"unavailable", staticPolicy{err: errors.New("offline")}, "example.com.", dnsmessage.TypeA, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := checkQuery(context.Background(), tc.source, queryFor(t, tc.host, tc.typ))
			if (answer == nil) != tc.allowed {
				t.Fatalf("allowed=%v, want %v", answer == nil, tc.allowed)
			}
			if !tc.allowed {
				var msg dnsmessage.Message
				if err := msg.Unpack(answer); err != nil {
					t.Fatal(err)
				}
				if msg.RCode != dnsmessage.RCodeNameError || !msg.Response || msg.ID != 17 {
					t.Fatalf("bad denial: %+v", msg.Header)
				}
			}
		})
	}
}
func TestQueryPolicyRejectsEveryQuestionAndMalformed(t *testing.T) {
	var msg dnsmessage.Message
	if err := msg.Unpack(queryFor(t, "example.com.", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	blocked, _ := dnsmessage.NewName("blocked.test.")
	msg.Questions = append(msg.Questions, dnsmessage.Question{Name: blocked, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET})
	multi, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range [][]byte{multi, {}, {0, 1, 2}, queryFor(t, "*.example.com.", dnsmessage.TypeA)} {
		if checkQuery(context.Background(), policyFor("example.com"), q) == nil {
			t.Fatal("unsafe query was allowed")
		}
	}
}
func TestRelayPolicyBlocksBeforeUpstreamUDP(t *testing.T) {
	var forwarded atomic.Int32
	upstream := newFakeResolver(t, func(query []byte) []byte { forwarded.Add(1); return dnsAnswer(query, 0) })
	relay, err := NewRelayForUpstreams([]string{upstream})
	if err != nil {
		t.Fatal(err)
	}
	_, addr, _ := serveLoopbackPolicy(t, relay, policyFor("example.com"))
	conn, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, host := range []string{"blocked.test.", "example.com.", "secret.example.com."} {
		if _, err := conn.Write(queryFor(t, host, dnsmessage.TypeTXT)); err != nil {
			t.Fatal(err)
		}
		var msg dnsmessage.Message
		if err := msg.Unpack(readWithin(t, conn)); err != nil {
			t.Fatal(err)
		}
		if (msg.RCode == dnsmessage.RCodeSuccess) != (host == "example.com.") {
			t.Fatalf("%s: %+v", host, msg.Header)
		}
	}
	if forwarded.Load() != 1 {
		t.Fatalf("forwarded %d queries, want 1", forwarded.Load())
	}
}
func TestRelayPolicyChecksEachTCPMessage(t *testing.T) {
	relay, err := NewRelayForUpstreams([]string{newTCPEchoResolver(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, addr := serveLoopbackPolicy(t, relay, policyFor("example.com"))
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	for _, host := range []string{"blocked.test.", "example.com.", "secret.example.com.", "example.com."} {
		if _, err := conn.Write(tcpStream(queryFor(t, host, dnsmessage.TypeA))); err != nil {
			t.Fatal(err)
		}
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			t.Fatal(err)
		}
		answer := make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err := io.ReadFull(conn, answer); err != nil {
			t.Fatal(err)
		}
		var msg dnsmessage.Message
		if err := msg.Unpack(answer); err != nil {
			t.Fatal(err)
		}
		if (msg.RCode == dnsmessage.RCodeSuccess) != (host == "example.com.") {
			t.Fatalf("%s: %+v", host, msg.Header)
		}
	}
}

type changingPolicy struct {
	current atomic.Pointer[ateletpb.EgressPolicy]
}

func (p *changingPolicy) EgressPolicy(context.Context) (*ateletpb.EgressPolicy, error) {
	return p.current.Load(), nil
}
func TestQueryPolicyFollowsChanges(t *testing.T) {
	source := &changingPolicy{}
	q := queryFor(t, "example.com.", dnsmessage.TypeA)
	for _, p := range []*ateletpb.EgressPolicy{policyFor("example.com").policy, policyFor("other.test").policy, nil, policyFor("example.com").policy} {
		source.current.Store(p)
		got := checkQuery(context.Background(), source, q) == nil
		want := p != nil && p.GetRules()[0].GetHttps().GetHostnames()[0] == "example.com"
		if got != want {
			t.Fatalf("allowed=%v, want %v", got, want)
		}
	}
}

func TestQueryPolicyAcceptsEverySupportedProtocol(t *testing.T) {
	dest := &ateletpb.EgressDestinations{Hostnames: []string{"example.com"}}
	for _, rule := range []*ateletpb.EgressRule{{Http: dest}, {Https: dest}, {TlsPassthrough: dest}, {Tcp: dest}} {
		source := staticPolicy{policy: &ateletpb.EgressPolicy{Rules: []*ateletpb.EgressRule{rule}}}
		if checkQuery(context.Background(), source, queryFor(t, "example.com.", dnsmessage.TypeA)) != nil {
			t.Fatalf("rule %v denied its host", rule)
		}
		if checkQuery(context.Background(), source, queryFor(t, "blocked.test.", dnsmessage.TypeA)) == nil {
			t.Fatalf("rule %v allowed another host", rule)
		}
	}
}
