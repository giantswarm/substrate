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
	"golang.org/x/net/dns/dnsmessage"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPolicyAllowsKubernetesSearchExpansion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 127.0.0.1\nsearch kagent.svc.cluster.local svc.cluster.local cluster.local evil.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	relay, err := NewRelay(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		policy, query string
		allowed       bool
	}{
		{"kagent-controller.kagent", "kagent-controller.kagent.svc.cluster.local.", true},
		{"agentlab-mockllm.agent-platform.svc", "agentlab-mockllm.agent-platform.svc.cluster.local.", true},
		{"kagent-controller.kagent", "kagent-controller.kagent.evil.test.", false},
		{"example.com", "secret.example.com.svc.cluster.local.", false},
		{"kagent-controller.kagent", "other.kagent.svc.cluster.local.", false},
		{"example.com", "example.com.evil.test.", false},
	} {
		got := checkQuery(context.Background(), policyFor(tc.policy), queryFor(t, tc.query, dnsmessage.TypeA), relay.searchDomains...) == nil
		if got != tc.allowed {
			t.Errorf("policy %s query %s allowed=%v, want %v", tc.policy, tc.query, got, tc.allowed)
		}
	}
}

func TestOnlyClusterSearchSuffixesAreTrusted(t *testing.T) {
	for _, tc := range []struct {
		search string
		want   []string
	}{
		{"search corp.example evil.test", nil},
		{"search svc.corp.example", nil},
		{"search team.svc.internal.test svc.internal.test internal.test corp.example", []string{"team.svc.internal.test", "svc.internal.test", "internal.test"}},
		{"search TEAM.SVC.CLUSTER.LOCAL. SVC.CLUSTER.LOCAL. CLUSTER.LOCAL. # comment", []string{"team.svc.cluster.local", "svc.cluster.local", "cluster.local"}},
	} {
		path := filepath.Join(t.TempDir(), "resolv.conf")
		if err := os.WriteFile(path, []byte("nameserver 127.0.0.1\n"+tc.search+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := resolvConfSearchDomains(path)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: domains %v, want %v", tc.search, got, tc.want)
		}
	}
}
