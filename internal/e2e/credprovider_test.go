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

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type testPolicyFile struct {
	Policies []struct {
		Atespace          string         `json:"atespace"`
		AllowedNamespaces []string       `json:"allowedNamespaces"`
		SecretSelector    map[string]any `json:"secretSelector,omitempty"`
	} `json:"policies"`
}

func parseTestPolicy(t *testing.T, policy string) testPolicyFile {
	t.Helper()
	var file testPolicyFile
	require.NoError(t, yaml.UnmarshalStrict([]byte(policy), &file))
	return file
}

func TestAddCredentialFixtureGrants(t *testing.T) {
	fixtureGrants := func(file testPolicyFile) int {
		n := 0
		for _, p := range file.Policies {
			for _, atespace := range credentialFixtureAtespaces {
				if p.Atespace == atespace && len(p.AllowedNamespaces) == 1 && p.AllowedNamespaces[0] == CredentialSecretsNamespace {
					n++
				}
			}
		}
		return n
	}

	t.Run("shipped empty policy", func(t *testing.T) {
		got, err := addCredentialFixtureGrants("# comment\npolicies: []\n")
		require.NoError(t, err)
		file := parseTestPolicy(t, got)
		require.Len(t, file.Policies, len(credentialFixtureAtespaces))
		require.Equal(t, len(credentialFixtureAtespaces), fixtureGrants(file))
	})

	t.Run("keeps the install's grants", func(t *testing.T) {
		installed := "policies:\n" +
			"- atespace: ate-e2e-probe-credentials\n" +
			"  allowedNamespaces: [ate-e2e-probe-credentials]\n" +
			"- atespace: team-a\n" +
			"  allowedNamespaces: [ns1]\n" +
			"  secretSelector:\n" +
			"    matchLabels:\n" +
			"      app: a\n"
		got, err := addCredentialFixtureGrants(installed)
		require.NoError(t, err)
		file := parseTestPolicy(t, got)
		require.Len(t, file.Policies, 2+len(credentialFixtureAtespaces))
		require.Equal(t, "ate-e2e-probe-credentials", file.Policies[0].Atespace)
		require.Equal(t, []string{"ate-e2e-probe-credentials"}, file.Policies[0].AllowedNamespaces)
		require.Equal(t, "team-a", file.Policies[1].Atespace)
		require.Equal(t, map[string]any{"matchLabels": map[string]any{"app": "a"}}, file.Policies[1].SecretSelector)
	})

	t.Run("adds a grant once", func(t *testing.T) {
		once, err := addCredentialFixtureGrants("policies: []\n")
		require.NoError(t, err)
		twice, err := addCredentialFixtureGrants(once)
		require.NoError(t, err)
		require.Equal(t, once, twice)
	})

	t.Run("rejects a malformed policy", func(t *testing.T) {
		_, err := addCredentialFixtureGrants("policies: {")
		require.Error(t, err)
	})
}
