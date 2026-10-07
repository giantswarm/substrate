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

package networking

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"sigs.k8s.io/yaml"
)

// TestKubectlAteEgressPolicy: this tree's kubectl-ate creates, reads back and
// replaces an actor's egress policy against this tree's ate-api, and the
// control plane stores the rules the manifests named, in this contract's
// shape: a hostnames rule is a hostnames rule, a cidrs rule a cidrs rule. The
// rules are checked over gRPC, not through the client under test. The actor is
// never resumed: the policy is a control-plane object.
func TestKubectlAteEgressPolicy(t *testing.T) {
	ctx := context.Background()
	clients := e2e.GetClients()
	template := egressFixture()
	actorName := fmt.Sprintf("egress-cli-%d", time.Now().UnixNano())
	actorRef := &ateapipb.ObjectRef{Atespace: networkingAtespace, Name: actorName}

	_, _ = clients.SubstrateAPI.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{
		Atespace: &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: networkingAtespace}},
	})
	actor := &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: networkingAtespace, Name: actorName},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: template.Namespace, Name: template.Name},
	}
	if _, err := clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: actor}); err != nil {
		t.Fatalf("CreateActor from %s/%s: %v (deploy the fixture with %s)", template.Namespace, template.Name, err, template.DeployWith)
	}
	t.Cleanup(func() {
		_, _ = clients.SubstrateAPI.DeleteActor(context.Background(), &ateapipb.DeleteActorRequest{Actor: actorRef})
	})

	// create: a manifest without metadata, the rule by hostname.
	const allowed = "api.example.com"
	manifest := fmt.Sprintf("rules:\n- hostnames:\n    patterns:\n    - %s\n", allowed)
	out, err := e2e.KubectlAte(t, strings.NewReader(manifest), "create", "egress-policy", actorName, "-a", networkingAtespace, "-f", "-", "-o", "json")
	if err != nil {
		t.Fatalf("create egress-policy: %v", err)
	}
	created := unmarshalEgressPolicy(t, out)
	wantRules := []*ateapipb.EgressRule{e2e.EgressAllowHostnames(allowed)}
	if diff := cmp.Diff(wantRules, created.GetRules(), protocmp.Transform()); diff != "" {
		t.Fatalf("create egress-policy printed other rules than the manifest named (-want +got):\n%s", diff)
	}
	stored, err := clients.SubstrateAPI.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actorRef})
	if err != nil {
		t.Fatalf("GetActorEgressPolicy after create: %v", err)
	}
	if diff := cmp.Diff(wantRules, stored.GetRules(), protocmp.Transform()); diff != "" {
		t.Fatalf("the control plane stores other rules than the manifest named (-want +got):\n%s", diff)
	}
	t.Logf("created the egress policy of %s/%s: version %d, %d rule", networkingAtespace, actorName, stored.GetMetadata().GetVersion(), len(stored.GetRules()))

	// get, edit, update: the document get prints is the manifest update takes,
	// its uid and version the preconditions; the rules become one by address.
	out, err = e2e.KubectlAte(t, nil, "get", "egress-policy", actorName, "-a", networkingAtespace, "-o", "yaml")
	if err != nil {
		t.Fatalf("get egress-policy: %v", err)
	}
	edited := unmarshalEgressPolicy(t, out)
	if edited.GetMetadata().GetUid() != stored.GetMetadata().GetUid() || edited.GetMetadata().GetVersion() != stored.GetMetadata().GetVersion() {
		t.Fatalf("get egress-policy printed uid %q version %d, the control plane holds uid %q version %d", edited.GetMetadata().GetUid(), edited.GetMetadata().GetVersion(), stored.GetMetadata().GetUid(), stored.GetMetadata().GetVersion())
	}
	const cidr = "192.0.2.0/24"
	edited.Rules = []*ateapipb.EgressRule{e2e.EgressAllowCIDRs(cidr)}
	body, err := protojson.Marshal(edited)
	if err != nil {
		t.Fatalf("marshal the edited policy: %v", err)
	}
	out, err = e2e.KubectlAte(t, bytes.NewReader(body), "update", "egress-policy", actorName, "-a", networkingAtespace, "-f", "-", "-o", "json")
	if err != nil {
		t.Fatalf("update egress-policy: %v", err)
	}
	updated := unmarshalEgressPolicy(t, out)
	wantRules = []*ateapipb.EgressRule{e2e.EgressAllowCIDRs(cidr)}
	if diff := cmp.Diff(wantRules, updated.GetRules(), protocmp.Transform()); diff != "" {
		t.Fatalf("update egress-policy printed other rules than the manifest named (-want +got):\n%s", diff)
	}
	stored2, err := clients.SubstrateAPI.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actorRef})
	if err != nil {
		t.Fatalf("GetActorEgressPolicy after update: %v", err)
	}
	if diff := cmp.Diff(wantRules, stored2.GetRules(), protocmp.Transform()); diff != "" {
		t.Fatalf("the control plane stores other rules than the update named (-want +got):\n%s", diff)
	}
	if stored2.GetMetadata().GetUid() != stored.GetMetadata().GetUid() || stored2.GetMetadata().GetVersion() <= stored.GetMetadata().GetVersion() {
		t.Fatalf("update left uid %q version %d, want the same uid %q and a version above %d", stored2.GetMetadata().GetUid(), stored2.GetMetadata().GetVersion(), stored.GetMetadata().GetUid(), stored.GetMetadata().GetVersion())
	}
	t.Logf("updated the egress policy of %s/%s: version %d, %d rule", networkingAtespace, actorName, stored2.GetMetadata().GetVersion(), len(stored2.GetRules()))
}

// unmarshalEgressPolicy decodes the YAML or JSON document kubectl-ate printed.
func unmarshalEgressPolicy(t *testing.T, doc []byte) *ateapipb.EgressPolicy {
	t.Helper()
	jsonDoc, err := yaml.YAMLToJSON(doc)
	if err != nil {
		t.Fatalf("kubectl-ate printed no YAML or JSON document: %v\n%s", err, doc)
	}
	policy := &ateapipb.EgressPolicy{}
	if err := protojson.Unmarshal(jsonDoc, policy); err != nil {
		t.Fatalf("kubectl-ate printed no EgressPolicy: %v\n%s", err, doc)
	}
	return policy
}
