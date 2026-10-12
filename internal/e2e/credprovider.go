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
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/agent-substrate/substrate/internal/installdefaults"
)

// TODO(yufan-su): Move these helpers into an internal/e2e/credprovider package.

const (
	// CredentialSecretsNamespace holds the fixture Secret and is the only
	// namespace the credinject fixture's authorization policy allows the probe
	// atespaces to resolve.
	CredentialSecretsNamespace = "ate-e2e-credinject-secrets"

	// CredentialInjectionURI resolves to CredentialInjectionToken through the
	// k8s-credential-provider, for the atespaces the fixture policy allows.
	CredentialInjectionURI = "ate-secret://k8s.io/default/" + CredentialSecretsNamespace + "/api-token/token"

	// CredentialInjectionToken is the fixture Secret's value, what an injected
	// header carries after any prefix.
	CredentialInjectionToken = "e2e-cred-inject-token"
)

// The fixture manifest (the Secret behind CredentialInjectionURI) and the
// provider's authorization policy: the ConfigMap the k8s-credential-provider
// Deployment mounts and the file in it.
const (
	credinjectFixtureManifest        = "internal/e2e/fixtures/credinject/credinject.yaml"
	credentialProviderPolicyName     = "k8s-credential-provider-namespace-policy"
	credentialProviderPolicyKey      = "namespace-policy.yaml"
	credentialProviderRestoreTimeout = time.Minute
)

// credentialFixtureAtespaces are the probe atespaces the fixture lets resolve
// CredentialSecretsNamespace. The suffixes are deterministic per sandbox class
// (see DeployProbe and probe-template.yaml.tmpl), so one grant set serves the
// gVisor and micro-VM variants.
var credentialFixtureAtespaces = []string{
	"ate-e2e-probe-egresscredinject",
	"ate-e2e-probe-microvm-egresscredinject",
}

// ConfigureCredentialProvider points the install's k8s-credential-provider at
// the credinject fixture: the Secret behind CredentialInjectionURI, and grants
// that let the probe atespaces resolve it, added to the authorization policy
// the install holds. The provider Deployment is restarted after the policy
// ConfigMap is updated because it reads the policy once at startup. When the
// test passes the fixture is removed, the policy put back exactly as it was
// found, and the provider restarted again so it drops the fixture's grants.
//
// The policy is shared by every suite that runs against the install, in
// parallel packages included: another suite may hold grants of its own in it
// (the credentials suite's come from the Helm values the install was made
// with). The fixture only adds, and its cleanup restores what it found rather
// than the policy the install ships, so no grant another suite relies on is
// revoked while it runs.
//
// A failed test keeps the fixture so the provider's logs can be read against
// it; the next run finds its grants in place and adds none.
//
// The provider itself and the gateway's side of the connection — the
// credential-provider configuration — are the install's
// (hack/install-ate.sh --credential-provider='{"name":"k8s.io"}'), not
// something this helper can retrofit: the rollout wait below fails on a
// cluster installed without them.
func ConfigureCredentialProvider(t *testing.T) {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}

	kubectl := func(args ...string) {
		if KubeContext != "" {
			args = append([]string{"--context=" + KubeContext}, args...)
		}
		RunCmd(t, "kubectl", args...)
	}

	// The manifests pin the canonical namespace.
	ns := installdefaults.SystemNamespace
	restart := func() {
		kubectl("-n", ns, "rollout", "restart", "deployment/k8s-credential-provider")
	}
	configMaps := GetClients().K8s.CoreV1().ConfigMaps(ns)

	fixture := filepath.Join(root, credinjectFixtureManifest)
	kubectl("apply", "-f", fixture)

	policy, err := configMaps.Get(t.Context(), credentialProviderPolicyName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get the credential provider policy: %v", err)
	}
	installed := policy.Data[credentialProviderPolicyKey]
	granted, err := addCredentialFixtureGrants(installed)
	if err != nil {
		t.Fatalf("add the fixture's grants to the credential provider policy: %v", err)
	}
	if policy.Data == nil {
		policy.Data = map[string]string{}
	}
	policy.Data[credentialProviderPolicyKey] = granted
	if _, err := configMaps.Update(t.Context(), policy, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update the credential provider policy: %v", err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		// t.Context() is already canceled when cleanups run.
		ctx, cancel := context.WithTimeout(context.Background(), credentialProviderRestoreTimeout)
		defer cancel()
		policy, err := configMaps.Get(ctx, credentialProviderPolicyName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("get the credential provider policy: %v", err)
			return
		}
		policy.Data[credentialProviderPolicyKey] = installed
		if _, err := configMaps.Update(ctx, policy, metav1.UpdateOptions{}); err != nil {
			t.Errorf("restore the credential provider policy: %v", err)
			return
		}
		kubectl("delete", "--ignore-not-found", "-f", fixture)
		// Not waiting for this rollout keeps the new pod's startup out of the
		// run. The old pod, still holding the fixture's grants, is replaced
		// once the new one is ready.
		restart()
	})

	restart()
	kubectl("-n", ns, "rollout", "status", "deployment/k8s-credential-provider", "--timeout=3m")
}

// addCredentialFixtureGrants returns the provider policy file with a grant of
// CredentialSecretsNamespace for each of credentialFixtureAtespaces appended.
// The grants already in the file are kept as they are, fields this package
// does not know included; a fixture grant the file already holds is not added
// twice.
func addCredentialFixtureGrants(policy string) (string, error) {
	var file struct {
		Policies []map[string]any `json:"policies"`
	}
	if err := yaml.Unmarshal([]byte(policy), &file); err != nil {
		return "", err
	}
	for _, atespace := range credentialFixtureAtespaces {
		grant := map[string]any{
			"atespace":          atespace,
			"allowedNamespaces": []any{CredentialSecretsNamespace},
		}
		if !slices.ContainsFunc(file.Policies, func(p map[string]any) bool { return reflect.DeepEqual(p, grant) }) {
			file.Policies = append(file.Policies, grant)
		}
	}
	out, err := yaml.Marshal(file)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
