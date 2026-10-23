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

package credentialprovider

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
)

// writeDockerConfig writes a .dockerconfigjson file and returns its path.
func writeDockerConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".dockerconfigjson")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func basicAuth(user, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
}

// candidateUsers returns the usernames of kc's candidates for ref, in order.
func candidateUsers(t *testing.T, kc CandidateSource, ref string) []string {
	t.Helper()
	candidates, err := kc.Candidates(context.Background(), repo(t, ref))
	if err != nil {
		t.Fatalf("Candidates(%q) returned unexpected error: %v", ref, err)
	}
	var users []string
	for _, c := range candidates {
		cfg, err := c.Authorization()
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, cfg.Username)
	}
	return users
}

func TestDockerConfigKeychainMatchesLikeTheKubelet(t *testing.T) {
	kc, err := NewDockerConfig([]string{writeDockerConfig(t, `{"auths": {
		"registry.example.com": {"auth": "`+basicAuth("registry", "p1")+`"},
		"https://registry.example.com/team/": {"username": "team", "password": "p2"},
		"*.mirror.example.com": {"username": "mirror", "password": "p3"},
		"https://index.docker.io/v1/": {"username": "hub", "password": "p4"},
		"other.example.com:5000": {"username": "port", "password": "p5"}
	}}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref  string
		want []string
	}{
		{"registry.example.com/app:1", []string{"registry"}},
		{"registry.example.com/team/app:1", []string{"team", "registry"}},
		{"eu.mirror.example.com/app:1", []string{"mirror"}},
		{"a.b.mirror.example.com/app:1", nil},
		{"busybox:1", []string{"hub"}},
		{"docker.io/library/busybox:1", []string{"hub"}},
		{"other.example.com:5000/app:1", []string{"port"}},
		{"other.example.com/app:1", nil},
		{"unknown.example.com/app:1", nil},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			if got := candidateUsers(t, kc, tc.ref); !slices.Equal(got, tc.want) {
				t.Errorf("candidates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDockerConfigKeychainDecodesAuthField(t *testing.T) {
	kc, err := NewDockerConfig([]string{writeDockerConfig(t,
		`{"auths": {"registry.example.com": {"auth": "`+basicAuth("user", "secret:with:colons")+`"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := kc.Resolve(repo(t, "registry.example.com/app:1"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := auth.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "user" || cfg.Password != "secret:with:colons" {
		t.Errorf("Authorization() = %q/%q, want the decoded auth field", cfg.Username, cfg.Password)
	}
}

func TestDockerConfigKeychainTriesFilesInOrderWithoutDuplicates(t *testing.T) {
	first := writeDockerConfig(t, `{"auths": {"registry.example.com": {"username": "first", "password": "p"}}}`)
	second := writeDockerConfig(t, `{"auths": {
		"registry.example.com": {"username": "second", "password": "p"},
		"*.example.com": {"auth": "`+basicAuth("first", "p")+`"}
	}}`)
	kc, err := NewDockerConfig([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := candidateUsers(t, kc, "registry.example.com/app:1"), []string{"first", "second"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestDockerConfigKeychainKeepsKeysThatNormalizeAlike(t *testing.T) {
	kc, err := NewDockerConfig([]string{writeDockerConfig(t, `{"auths": {
		"registry.example.com": {"username": "plain", "password": "p"},
		"https://registry.example.com/": {"username": "scheme", "password": "p"}
	}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := candidateUsers(t, kc, "registry.example.com/app:1"), []string{"scheme", "plain"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestDockerConfigKeychainRereadsRotatedFile(t *testing.T) {
	path := writeDockerConfig(t, `{"auths": {"registry.example.com": {"username": "old", "password": "p"}}}`)
	kc, err := NewDockerConfig([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"auths": {"registry.example.com": {"username": "new", "password": "p"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := candidateUsers(t, kc, "registry.example.com/app:1"), []string{"new"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}

	// A file that goes bad after start-up is skipped, not fatal.
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := candidateUsers(t, kc, "registry.example.com/app:1"); len(got) != 0 {
		t.Errorf("candidates = %v, want none from an unparsable file", got)
	}
}

func TestNewDockerConfigRejectsBadFiles(t *testing.T) {
	for name, path := range map[string]string{
		"missing":     filepath.Join(t.TempDir(), "absent"),
		"not json":    writeDockerConfig(t, "{"),
		"bad base64":  writeDockerConfig(t, `{"auths": {"registry.example.com": {"auth": "!!"}}}`),
		"no password": writeDockerConfig(t, `{"auths": {"registry.example.com": {"auth": "`+base64.StdEncoding.EncodeToString([]byte("user"))+`"}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDockerConfig([]string{path}); err == nil {
				t.Error("NewDockerConfig succeeded, want an error")
			}
		})
	}
}

// staticSource returns fixed candidates.
type staticSource []string

func (s staticSource) Candidates(context.Context, authn.Resource) ([]authn.Authenticator, error) {
	var candidates []authn.Authenticator
	for _, user := range s {
		candidates = append(candidates, authn.FromConfig(authn.AuthConfig{Username: user, Password: "p"}))
	}
	return candidates, nil
}

func TestChainTriesKeychainsInOrder(t *testing.T) {
	chain := Chain{staticSource{"pull-secret"}, staticSource{"provider-a", "provider-b"}}
	if got, want := candidateUsers(t, chain, "registry.example.com/app:1"), []string{"pull-secret", "provider-a", "provider-b"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
	auth, err := Chain{staticSource{}}.Resolve(repo(t, "registry.example.com/app:1"))
	if err != nil {
		t.Fatal(err)
	}
	if auth != authn.Anonymous {
		t.Errorf("Resolve() = %v, want anonymous without candidates", auth)
	}
}
