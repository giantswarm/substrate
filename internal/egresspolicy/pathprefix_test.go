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

package egresspolicy

import (
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

func TestValidatePathPrefix(t *testing.T) {
	for _, prefix := range []string{"/", "/api", "/api/", "/api/v4/projects", "/a;b/c", "/%41pi", "/~user/repo.git"} {
		if err := ValidatePathPrefix(prefix); err != nil {
			t.Errorf("ValidatePathPrefix(%q) = %v, want nil", prefix, err)
		}
	}
	for _, prefix := range []string{
		"api/", "*", "//", "/api//v4", "/api/./v4", "/api/../v4", "/..", "/.", "/api/..;/v4",
		`/api\v4`, "/api%2fv4", "/api%2Fv4", "/%2e%2e/", "/api%5cv4", "/api?x=1", "/api#x",
		"/api v4", "/api/\x00", "/café",
	} {
		if err := ValidatePathPrefix(prefix); err == nil {
			t.Errorf("ValidatePathPrefix(%q) = nil, want an error", prefix)
		}
	}
}

func TestPathPrefixMatches(t *testing.T) {
	for _, tc := range []struct {
		prefix, path string
		want         bool
	}{
		{"/api", "/api", true},
		{"/api", "/api/v4", true},
		{"/api", "/apis", false},
		{"/api/", "/api/v4", true},
		{"/api/", "/api/", true},
		{"/api/", "/api", false},
		{"/", "/anything", true},
		{"/api/v4", "/api", false},
	} {
		if got := PathPrefixMatches(tc.prefix, tc.path); got != tc.want {
			t.Errorf("PathPrefixMatches(%q, %q) = %v, want %v", tc.prefix, tc.path, got, tc.want)
		}
	}
}

// ReplaceHeadersFor picks, per header name, the entry with the longest
// matching path prefix, the entry without one otherwise.
func TestReplaceHeadersFor(t *testing.T) {
	basic := &ateapipb.CredentialHeader{Header: "Authorization", Prefix: "Basic ", CredentialUri: "ate-secret://p/basic"}
	bearer := &ateapipb.CredentialHeader{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://p/bearer", PathPrefix: "/api/"}
	v4 := &ateapipb.CredentialHeader{Header: "AUTHORIZATION", Prefix: "Token ", CredentialUri: "ate-secret://p/v4", PathPrefix: "/api/v4"}
	apiKey := &ateapipb.CredentialHeader{Header: "X-Api-Key", CredentialUri: "ate-secret://p/key", PathPrefix: "/api/"}
	effects := &ateapipb.HttpRuleEffects{ReplaceHeaders: []*ateapipb.CredentialHeader{bearer, basic, apiKey, v4}}

	for _, tc := range []struct {
		target string
		want   []*ateapipb.CredentialHeader
	}{
		{"/repo.git/info/refs?service=git-upload-pack", []*ateapipb.CredentialHeader{basic}},
		{"/api/users", []*ateapipb.CredentialHeader{bearer, apiKey}},
		{"/api/v4/projects?per_page=1", []*ateapipb.CredentialHeader{v4, apiKey}},
		{"/api/v4", []*ateapipb.CredentialHeader{v4, apiKey}},
		{"/api/v40", []*ateapipb.CredentialHeader{bearer, apiKey}},
		{"/api", []*ateapipb.CredentialHeader{basic}},
		{"/api/?/v4", []*ateapipb.CredentialHeader{bearer, apiKey}},
		// Paths an origin could resolve elsewhere get only the entry
		// without a prefix.
		{"/api/../admin", []*ateapipb.CredentialHeader{basic}},
		{"/api/v4/..;/x", []*ateapipb.CredentialHeader{basic}},
		{"/api//v4", []*ateapipb.CredentialHeader{basic}},
		{"/api/%2e%2e/admin", []*ateapipb.CredentialHeader{basic}},
		{`/api\v4`, []*ateapipb.CredentialHeader{basic}},
		{"https://git.example.com/api/v4", []*ateapipb.CredentialHeader{basic}},
		{"", []*ateapipb.CredentialHeader{basic}},
	} {
		got := ReplaceHeadersFor(effects, tc.target)
		if len(got) != len(tc.want) {
			t.Errorf("ReplaceHeadersFor(%q) = %v, want %v", tc.target, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ReplaceHeadersFor(%q)[%d] = %v, want %v", tc.target, i, got[i], tc.want[i])
			}
		}
	}

	if got := ReplaceHeadersFor(nil, "/api/"); len(got) != 0 {
		t.Errorf("ReplaceHeadersFor(nil) = %v, want none", got)
	}
}

// A percent-encoded slash is canonical when reading it as a separator yields
// no dot or empty segment, so GitLab's encoded project, file and branch paths
// get the credential scoped above them.
func TestReplaceHeadersForEncodedSlash(t *testing.T) {
	api := &ateapipb.CredentialHeader{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://p/gitlab-api", PathPrefix: "/api/v4/"}
	scoped := &ateapipb.HttpRuleEffects{ReplaceHeaders: []*ateapipb.CredentialHeader{api}}

	for _, target := range []string{
		"/api/v4/projects/workspaces%2Fworkspace-proof",
		"/api/v4/projects/workspaces%2fworkspace-proof/merge_requests",
		"/api/v4/projects/group%2Fsub%2Fproject/repository/files/docs%2FREADME.md/raw",
		"/api/v4/projects/group%2Fproject/repository/branches/feature%252Fx",
	} {
		if got := ReplaceHeadersFor(scoped, target); len(got) != 1 || got[0] != api {
			t.Errorf("ReplaceHeadersFor(scoped, %q) = %v, want the scoped entry", target, got)
		}
	}
	// An encoded slash never moves a path under a prefix it is not under as
	// written.
	for _, target := range []string{"/api%2Fv4/projects", "/api/v4%2Fprojects", "/api%2fv4/user"} {
		if got := ReplaceHeadersFor(scoped, target); len(got) != 0 {
			t.Errorf("ReplaceHeadersFor(scoped, %q) = %v, want none", target, got)
		}
	}
}

// The request path is compared as written, so every form an origin could
// resolve outside a prefix it starts with matches no prefix: dot segments,
// percent-encoded dots and backslashes in either case, empty segments (a
// percent-encoded slash read as a separator too), path parameters and
// backslashes. Such a request gets the host-wide entry, none without one.
func TestReplaceHeadersForEscapes(t *testing.T) {
	codex := &ateapipb.CredentialHeader{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://p/codex", PathPrefix: "/backend-api/codex/"}
	host := &ateapipb.CredentialHeader{Header: "authorization", Prefix: "Basic ", CredentialUri: "ate-secret://p/host"}
	scoped := &ateapipb.HttpRuleEffects{ReplaceHeaders: []*ateapipb.CredentialHeader{codex}}
	withHost := &ateapipb.HttpRuleEffects{ReplaceHeaders: []*ateapipb.CredentialHeader{codex, host}}

	// The prefix itself still applies under it.
	if got := ReplaceHeadersFor(withHost, "/backend-api/codex/responses?stream=1"); len(got) != 1 || got[0] != codex {
		t.Fatalf("ReplaceHeadersFor under the prefix = %v, want the scoped entry", got)
	}

	for _, target := range []string{
		"/backend-api/codex/../../other",
		"/backend-api/codex/..",
		"/backend-api/codex/./../other",
		"/backend-api/codex/%2e%2e/%2e%2e/other",
		"/backend-api/codex/%2E%2E/%2E%2E/other",
		"/backend-api/codex/%2e%2E/other",
		"/backend-api/codex/.%2e/other",
		"/backend-api/codex/%2e./other",
		"/backend-api/codex/..%2fother",
		"/backend-api/codex/..%2Fother",
		"/backend-api/codex/%2fother",
		"/backend-api/codex%2f..%2f..%2fother",
		"/backend-api/codex//../../other",
		"/backend-api/codex//other",
		"/backend-api/codex/..;/..;/other",
		"/backend-api/codex/..;x=1/other",
		"/backend-api/codex/a%2f..%2f..%2fother",
		"/backend-api/codex/a%2F.%2Fother",
		"/backend-api/codex/a%2F%2Fother",
		"/backend-api/codex/a%2F..;x=1%2Fother",
		`/backend-api/codex/..\..\other`,
		"/backend-api/codex/..%5c..%5cother",
		"/backend-api/codex/..%5C..%5Cother",
		"/backend-api/codex/../../other?q=/backend-api/codex/",
	} {
		if got := ReplaceHeadersFor(scoped, target); len(got) != 0 {
			t.Errorf("ReplaceHeadersFor(scoped, %q) = %v, want none", target, got)
		}
		if got := ReplaceHeadersFor(withHost, target); len(got) != 1 || got[0] != host {
			t.Errorf("ReplaceHeadersFor(withHost, %q) = %v, want the host-wide entry", target, got)
		}
	}
}
