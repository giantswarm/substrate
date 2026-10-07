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

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/kubectl-ate/internal/printer"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEgressPolicyFromManifest(t *testing.T) {
	t.Parallel()

	fullMetadata := &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}
	httpRules := []*ateapipb.EgressRule{{Http: &ateapipb.HTTPRule{Hostnames: []string{"api.example.com"}}}}
	tlsRules := []*ateapipb.EgressRule{{TlsPassthrough: &ateapipb.TLSPassthroughRule{Hostnames: []string{"*.example.com"}, Ports: &ateapipb.Ports{Numbers: []int32{443}}}}}
	anyRules := []*ateapipb.EgressRule{{Http: &ateapipb.HTTPRule{Hostnames: []string{"*"}}}}

	tests := []struct {
		name     string
		manifest string
		want     *ateapipb.EgressPolicy
		wantErr  bool
		// wantErrContains is checked only for errors this package produces.
		wantErrContains string
	}{
		{
			name: "camel case http",
			manifest: `metadata:
  atespace: team-a
  name: default
rules:
- http:
    hostnames:
    - api.example.com
`,
			want: &ateapipb.EgressPolicy{Metadata: fullMetadata, Rules: httpRules},
		},
		{
			name: "snake case tls_passthrough",
			manifest: `metadata: {atespace: team-a, name: default}
rules:
- tls_passthrough: {hostnames: ["*.example.com"], ports: {numbers: [443]}}
`,
			want: &ateapipb.EgressPolicy{Metadata: fullMetadata, Rules: tlsRules},
		},
		{
			name: "star pattern",
			manifest: `metadata: {atespace: team-a, name: default}
rules:
- http: {hostnames: ["*"]}
`,
			want: &ateapipb.EgressPolicy{Metadata: fullMetadata, Rules: anyRules},
		},
		{
			name: "metadata omitted is left nil",
			manifest: `rules:
- http: {hostnames: [api.example.com]}
`,
			want: &ateapipb.EgressPolicy{Rules: httpRules},
		},
		{
			name: "uid version and timestamps preserved",
			manifest: `metadata:
  atespace: team-a
  name: default
  uid: 3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d
  version: "2"
  createTime: "2026-01-01T11:55:00Z"
rules:
- http: {hostnames: [api.example.com]}
`,
			want: &ateapipb.EgressPolicy{
				Metadata: &ateapipb.ResourceMetadata{
					Atespace:   "team-a",
					Name:       "default",
					Uid:        "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d",
					Version:    2,
					CreateTime: timestamppb.New(time.Date(2026, 1, 1, 11, 55, 0, 0, time.UTC)),
				},
				Rules: httpRules,
			},
		},
		{
			name:     "json input",
			manifest: `{"metadata": {"atespace": "team-a", "name": "default"}, "rules": [{"tls_passthrough": {"hostnames": ["*.example.com"], "ports": {"numbers": [443]}}}]}`,
			want:     &ateapipb.EgressPolicy{Metadata: fullMetadata, Rules: tlsRules},
		},
		{name: "empty", manifest: "", wantErr: true, wantErrContains: "manifest is empty"},
		{name: "unknown field", manifest: "rulez: []", wantErr: true, wantErrContains: "invalid EgressPolicy"},
		{name: "rules not a list", manifest: "rules: {http: {}}", wantErr: true},
		{
			name: "crd shape",
			manifest: `apiVersion: ate.dev/v1alpha1
kind: EgressPolicy
metadata: {name: default}
`,
			wantErr: true,
		},
		{name: "not yaml", manifest: "\t{", wantErr: true, wantErrContains: "invalid YAML"},
		{
			name: "leading document separator",
			manifest: `---
rules:
- http: {hostnames: ["*"]}
`,
			want: &ateapipb.EgressPolicy{Rules: anyRules},
		},
		{
			name: "document end marker",
			manifest: `rules:
- http: {hostnames: ["*"]}
...
`,
			want: &ateapipb.EgressPolicy{Rules: anyRules},
		},
		{
			name: "trailing document separator",
			manifest: `rules:
- http: {hostnames: ["*"]}
---
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "empty second document",
			manifest: `rules:
- http: {hostnames: ["*"]}
---
# nothing here
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "empty first document",
			manifest: `---
# nothing
---
rules:
- http: {hostnames: ["*"]}
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "two leading separators",
			manifest: `---
---
rules:
- http: {hostnames: ["*"]}
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "document end marker then separator",
			manifest: `rules:
- http: {hostnames: ["*"]}
...
---
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "two egress policies separated by ---",
			manifest: `metadata:
  atespace: team-a
  name: default
rules:
- http:
    hostnames:
    - api.example.com
---
metadata:
  atespace: team-b
  name: default
rules:
- tls_passthrough:
    hostnames:
    - "*.example.com"
    ports:
      numbers:
      - 443
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "three egress policies separated by ---",
			manifest: `metadata: {atespace: team-a, name: default}
rules:
- http: {hostnames: [api.example.com]}
---
metadata: {atespace: team-b, name: default}
rules:
- tls_passthrough: {hostnames: ["*.example.com"], ports: {numbers: [443]}}
---
metadata: {atespace: team-c, name: default}
rules:
- http: {hostnames: ["*"]}
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: "two policies with an empty document between",
			manifest: `metadata: {atespace: team-a, name: default}
rules:
- http: {hostnames: [api.example.com]}
---
# just a comment
---
metadata: {atespace: team-b, name: default}
rules:
- tls_passthrough: {hostnames: ["*.example.com"], ports: {numbers: [443]}}
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{
			name: `two json documents separated by ---`,
			manifest: `{"metadata": {"atespace": "team-a", "name": "default"}, "rules": [{"http": {"hostnames": ["*"]}}]}
---
{"metadata": {"atespace": "team-b", "name": "default"}, "rules": [{"http": {"hostnames": ["*"]}}]}
`,
			wantErr:         true,
			wantErrContains: "manifest holds more than one document",
		},
		{name: "only a separator", manifest: "---\n", wantErr: true, wantErrContains: "manifest is empty"},
		{name: "comment only", manifest: "# nothing\n", wantErr: true, wantErrContains: "manifest is empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := egressPolicyFromManifest([]byte(test.manifest))
			if (err != nil) != test.wantErr {
				t.Fatalf("egressPolicyFromManifest() error = %v, wantErr %t", err, test.wantErr)
			}
			if test.wantErr {
				if test.wantErrContains != "" && !strings.Contains(err.Error(), test.wantErrContains) {
					t.Fatalf("egressPolicyFromManifest() error = %v, want it to contain %q", err, test.wantErrContains)
				}
				return
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("policy mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOverrideEgressPolicyMetadata(t *testing.T) {
	t.Parallel()

	rules := []*ateapipb.EgressRule{{Http: &ateapipb.HTTPRule{Hostnames: []string{"*"}}}}
	filled := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, Rules: rules}
	pinned := &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Uid: "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d", Version: 2}

	tests := []struct {
		name            string
		policy          *ateapipb.EgressPolicy
		want            *ateapipb.EgressPolicy
		wantErrContains string
	}{
		{
			name:   "metadata omitted is filled",
			policy: &ateapipb.EgressPolicy{Rules: rules},
			want:   filled,
		},
		{
			name:   "empty metadata object is filled",
			policy: &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{}, Rules: rules},
			want:   filled,
		},
		{
			name:   "name omitted is filled",
			policy: &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a"}, Rules: rules},
			want:   filled,
		},
		{
			name:   "atespace omitted is filled",
			policy: &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Name: "default"}, Rules: rules},
			want:   filled,
		},
		{
			name:   "matching metadata is kept",
			policy: &ateapipb.EgressPolicy{Metadata: proto.Clone(pinned).(*ateapipb.ResourceMetadata), Rules: rules},
			want:   &ateapipb.EgressPolicy{Metadata: pinned, Rules: rules},
		},
		{
			name:            "atespace mismatch",
			policy:          &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: "dev"}, Rules: rules},
			wantErrContains: `metadata.atespace "dev" does not match --atespace "team-a"`,
		},
		{
			name:            "name mismatch",
			policy:          &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Name: "other"}, Rules: rules},
			wantErrContains: `metadata.name "other" must be "default"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := overrideEgressPolicyMetadata(test.policy, "team-a")
			if test.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrContains) {
					t.Fatalf("overrideEgressPolicyMetadata() error = %v, want it to contain %q", err, test.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("overrideEgressPolicyMetadata() error = %v, want nil", err)
			}
			if diff := cmp.Diff(test.want, test.policy, protocmp.Transform()); diff != "" {
				t.Errorf("policy mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A printed policy must decode back unchanged, so `get -o yaml` output can be
// fed to `create -f` as is.
func TestEgressPolicyManifest_RoundTrip(t *testing.T) {
	t.Parallel()

	policy := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace:   "team-a",
			Name:       "default",
			Uid:        "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d",
			Version:    2,
			CreateTime: timestamppb.New(time.Date(2026, 1, 1, 11, 55, 0, 0, time.UTC)),
			UpdateTime: timestamppb.New(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
		},
		Rules: []*ateapipb.EgressRule{
			{Http: &ateapipb.HTTPRule{Hostnames: []string{"*.example.com"}, Ports: &ateapipb.Ports{Numbers: []int32{80, 8080}}}},
			{Https: &ateapipb.HTTPSRule{Hostnames: []string{"api.example.com"}, Effects: &ateapipb.HttpRuleEffects{
				ReplaceHeaders: []*ateapipb.CredentialHeader{{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://k8s/default/token"}},
			}}},
			{TlsPassthrough: &ateapipb.TLSPassthroughRule{Hostnames: []string{"*"}, Ports: &ateapipb.Ports{All: &ateapipb.AllPorts{}}}},
		},
	}

	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := printer.PrintEgressPolicyTo(&buf, "c1", policy, format); err != nil {
				t.Fatalf("PrintEgressPolicyTo(%s) error = %v", format, err)
			}
			got, err := egressPolicyFromManifest(buf.Bytes())
			if err != nil {
				t.Fatalf("egressPolicyFromManifest(%q) error = %v", buf.String(), err)
			}
			if diff := cmp.Diff(policy, got, protocmp.Transform()); diff != "" {
				t.Errorf("round trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEgressPolicyCommandArgs(t *testing.T) {
	runCommandArgsTests(t, []commandArgsTest{
		{name: "get", command: getEgressPolicyCmd, args: []string{"c1"}},
		{name: "get requires actor", command: getEgressPolicyCmd, wantErr: true},
		{name: "get rejects multiple", command: getEgressPolicyCmd, args: []string{"c1", "c2"}, wantErr: true},
		{name: "create", command: createEgressPolicyCmd, args: []string{"c1"}},
		{name: "create requires actor", command: createEgressPolicyCmd, wantErr: true},
		{name: "create rejects multiple", command: createEgressPolicyCmd, args: []string{"c1", "c2"}, wantErr: true},
		{name: "update", command: updateEgressPolicyCmd, args: []string{"c1"}},
		{name: "update requires actor", command: updateEgressPolicyCmd, wantErr: true},
		{name: "update rejects multiple", command: updateEgressPolicyCmd, args: []string{"c1", "c2"}, wantErr: true},
	})
}

// fakeEgressPolicyGetter records the requests it received and answers with a
// configured policy or error. actorReq stays nil unless the runner reads the
// actor, which it only does after a NotFound.
type fakeEgressPolicyGetter struct {
	req      *ateapipb.GetActorEgressPolicyRequest
	policy   *ateapipb.EgressPolicy
	err      error
	actorReq *ateapipb.GetActorRequest
	actorErr error
}

func (f *fakeEgressPolicyGetter) GetActorEgressPolicy(ctx context.Context, req *ateapipb.GetActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.policy, nil
}

func (f *fakeEgressPolicyGetter) GetActor(ctx context.Context, req *ateapipb.GetActorRequest, opts ...grpc.CallOption) (*ateapipb.Actor, error) {
	f.actorReq = req
	if f.actorErr != nil {
		return nil, f.actorErr
	}
	return &ateapipb.Actor{}, nil
}

func TestGetEgressPolicyRunner_Run(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pinTime(t, now)

	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	policy := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace:   "team-a",
			Name:       "default",
			Uid:        "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d",
			Version:    1,
			CreateTime: timestamppb.New(now.Add(-5 * time.Minute)), // table row prints AGE 5m
		},
		Rules: []*ateapipb.EgressRule{{Http: &ateapipb.HTTPRule{Hostnames: []string{"api.example.com"}}}},
	}

	tests := []struct {
		name         string
		outputFmt    string
		getter       *fakeEgressPolicyGetter
		wantReq      *ateapipb.GetActorEgressPolicyRequest
		wantActorReq *ateapipb.GetActorRequest
		wantOut      string
		wantErrOut   string
		wantErr      string
	}{
		{
			name:      "table by default",
			outputFmt: "table",
			getter:    &fakeEgressPolicyGetter{policy: policy},
			wantReq:   &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantOut: `ATESPACE   ACTOR   RULES   VERSION   AGE
team-a     c1      1       1         5m
`,
		},
		{
			name:      "yaml",
			outputFmt: "yaml",
			getter:    &fakeEgressPolicyGetter{policy: policy},
			wantReq:   &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantOut: `metadata:
  atespace: team-a
  createTime: "2026-01-01T11:55:00Z"
  name: default
  uid: 3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d
  version: "1"
rules:
- http:
    hostnames:
    - api.example.com
`,
		},
		{
			name:         "no policy on an existing actor writes a note and succeeds",
			outputFmt:    "yaml",
			getter:       &fakeEgressPolicyGetter{err: status.Error(codes.NotFound, "EgressPolicy not found")},
			wantReq:      &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErrOut:   "actor \"c1\" in atespace \"team-a\" has no egress policy\n",
		},
		{
			name:         "missing actor fails",
			outputFmt:    "yaml",
			getter:       &fakeEgressPolicyGetter{err: status.Error(codes.NotFound, "EgressPolicy not found"), actorErr: status.Error(codes.NotFound, "Actor team-a/c1 not found")},
			wantReq:      &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErr:      `actor "c1" in atespace "team-a" not found`,
		},
		{
			name:         "actor lookup error wraps",
			outputFmt:    "yaml",
			getter:       &fakeEgressPolicyGetter{err: status.Error(codes.NotFound, "EgressPolicy not found"), actorErr: status.Error(codes.PermissionDenied, "denied")},
			wantReq:      &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErr:      `failed to get actor "c1" in atespace "team-a": rpc error: code = PermissionDenied desc = denied`,
		},
		{
			name:      "other error wraps",
			outputFmt: "table",
			getter:    &fakeEgressPolicyGetter{err: status.Error(codes.Unavailable, "api-server down")},
			wantReq:   &ateapipb.GetActorEgressPolicyRequest{Actor: actor},
			wantErr:   `failed to get egress policy for actor "c1" in atespace "team-a": rpc error: code = Unavailable desc = api-server down`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			runner := &getEgressPolicyRunner{
				getter:    test.getter,
				actor:     actor,
				outputFmt: test.outputFmt,
				stdout:    &stdout,
				stderr:    &stderr,
			}
			err := runner.Run(context.Background())
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != test.wantErr {
				t.Fatalf("Run() error = %q, want %q", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantReq, test.getter.req, protocmp.Transform()); diff != "" {
				t.Errorf("request mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantActorReq, test.getter.actorReq, protocmp.Transform()); diff != "" {
				t.Errorf("actor request mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantOut, stdout.String()); diff != "" {
				t.Errorf("stdout mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErrOut, stderr.String()); diff != "" {
				t.Errorf("stderr mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fakeEgressPolicyCreator records the request it received and answers with a
// configured policy or error.
type fakeEgressPolicyCreator struct {
	req    *ateapipb.CreateActorEgressPolicyRequest
	policy *ateapipb.EgressPolicy
	err    error
}

func (f *fakeEgressPolicyCreator) CreateActorEgressPolicy(ctx context.Context, req *ateapipb.CreateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.policy, nil
}

func TestCreateEgressPolicyRunner_Run(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pinTime(t, now)

	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	rules := []*ateapipb.EgressRule{{Http: &ateapipb.HTTPRule{Hostnames: []string{"api.example.com"}}}}
	// A manifest cloned from another actor still carries that actor's
	// server-managed fields; the CLI sends them as is and the server scrubs them.
	manifest := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Uid: "old-uid", Version: 7},
		Rules:    rules,
	}
	created := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace:   "team-a",
			Name:       "default",
			Uid:        "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d",
			Version:    1,
			CreateTime: timestamppb.New(now),
		},
		Rules: rules,
	}
	wantReq := &ateapipb.CreateActorEgressPolicyRequest{Actor: actor, EgressPolicy: manifest}

	tests := []struct {
		name      string
		outputFmt string
		creator   *fakeEgressPolicyCreator
		wantOut   string
		wantErr   string
	}{
		{
			name:      "table by default",
			outputFmt: "table",
			creator:   &fakeEgressPolicyCreator{policy: created},
			wantOut: `ATESPACE   ACTOR   RULES   VERSION   AGE
team-a     c1      1       1         0s
`,
		},
		{
			name:      "yaml prints the created policy",
			outputFmt: "yaml",
			creator:   &fakeEgressPolicyCreator{policy: created},
			wantOut: `metadata:
  atespace: team-a
  createTime: "2026-01-01T12:00:00Z"
  name: default
  uid: 3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d
  version: "1"
rules:
- http:
    hostnames:
    - api.example.com
`,
		},
		{
			name:      "already exists wraps",
			outputFmt: "table",
			creator:   &fakeEgressPolicyCreator{err: status.Error(codes.AlreadyExists, "EgressPolicy already exists")},
			wantErr:   `failed to create egress policy for actor "c1" in atespace "team-a": rpc error: code = AlreadyExists desc = EgressPolicy already exists`,
		},
		{
			name:      "missing actor wraps",
			outputFmt: "table",
			creator:   &fakeEgressPolicyCreator{err: status.Error(codes.FailedPrecondition, "parent Actor does not exist")},
			wantErr:   `failed to create egress policy for actor "c1" in atespace "team-a": rpc error: code = FailedPrecondition desc = parent Actor does not exist`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			runner := &createEgressPolicyRunner{
				creator:   test.creator,
				guard:     &fakeEgressPolicyGuard{version: ateapipb.EgressPolicyContractVersion},
				actor:     actor,
				policy:    manifest,
				outputFmt: test.outputFmt,
				stdout:    &stdout,
			}
			err := runner.Run(context.Background())
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != test.wantErr {
				t.Fatalf("Run() error = %q, want %q", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(wantReq, test.creator.req, protocmp.Transform()); diff != "" {
				t.Errorf("request mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantOut, stdout.String()); diff != "" {
				t.Errorf("stdout mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fakeEgressPolicyUpdater records the requests it received and answers with a
// configured policy or error. actorReq stays nil unless the runner reads the
// actor, which it only does after a NotFound.
type fakeEgressPolicyUpdater struct {
	req      *ateapipb.UpdateActorEgressPolicyRequest
	policy   *ateapipb.EgressPolicy
	err      error
	actorReq *ateapipb.GetActorRequest
	actorErr error
}

func (f *fakeEgressPolicyUpdater) UpdateActorEgressPolicy(ctx context.Context, req *ateapipb.UpdateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.policy, nil
}

func (f *fakeEgressPolicyUpdater) GetActor(ctx context.Context, req *ateapipb.GetActorRequest, opts ...grpc.CallOption) (*ateapipb.Actor, error) {
	f.actorReq = req
	if f.actorErr != nil {
		return nil, f.actorErr
	}
	return &ateapipb.Actor{}, nil
}

func TestUpdateEgressPolicyRunner_Run(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pinTime(t, now)

	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	const uid = "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d"
	rules := []*ateapipb.EgressRule{
		{Http: &ateapipb.HTTPRule{Hostnames: []string{"api.example.com"}}},
		{Https: &ateapipb.HTTPSRule{Hostnames: []string{"www.example.com"}}},
	}
	// The manifest is what `get -o yaml` printed, edited: it still carries the
	// uid, version, and timestamps of the policy being replaced.
	manifest := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace:   "team-a",
			Name:       "default",
			Uid:        uid,
			Version:    1,
			CreateTime: timestamppb.New(now.Add(-time.Minute)),
		},
		Rules: rules,
	}
	updated := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace:   "team-a",
			Name:       "default",
			Uid:        uid,
			Version:    2,
			CreateTime: timestamppb.New(now.Add(-time.Minute)),
			UpdateTime: timestamppb.New(now),
		},
		Rules: rules,
	}
	wantReq := &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: manifest}

	tests := []struct {
		name         string
		outputFmt    string
		updater      *fakeEgressPolicyUpdater
		wantActorReq *ateapipb.GetActorRequest
		wantOut      string
		wantErr      string
	}{
		{
			name:      "table by default",
			outputFmt: "table",
			updater:   &fakeEgressPolicyUpdater{policy: updated},
			wantOut: `ATESPACE   ACTOR   RULES   VERSION   AGE
team-a     c1      2       2         60s
`,
		},
		{
			name:      "yaml prints the updated policy",
			outputFmt: "yaml",
			updater:   &fakeEgressPolicyUpdater{policy: updated},
			wantOut: `metadata:
  atespace: team-a
  createTime: "2026-01-01T11:59:00Z"
  name: default
  uid: 3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d
  updateTime: "2026-01-01T12:00:00Z"
  version: "2"
rules:
- http:
    hostnames:
    - api.example.com
- https:
    hostnames:
    - www.example.com
`,
		},
		{
			name:      "stale version tells the user to re-read",
			outputFmt: "table",
			updater:   &fakeEgressPolicyUpdater{err: status.Error(codes.Aborted, "EgressPolicy version conflict")},
			wantErr:   `manifest's uid and version preconditions do not match those of the stored egress policy for actor "c1" in atespace "team-a" (changed since it was read, or read from another actor's policy); re-run "get egress-policy -o yaml" and reapply the edit: rpc error: code = Aborted desc = EgressPolicy version conflict`,
		},
		{
			name:      "uid conflict tells the user to re-read",
			outputFmt: "table",
			updater:   &fakeEgressPolicyUpdater{err: status.Error(codes.Aborted, "EgressPolicy UID conflict")},
			wantErr:   `manifest's uid and version preconditions do not match those of the stored egress policy for actor "c1" in atespace "team-a" (changed since it was read, or read from another actor's policy); re-run "get egress-policy -o yaml" and reapply the edit: rpc error: code = Aborted desc = EgressPolicy UID conflict`,
		},
		{
			name:         "missing actor names the actor",
			outputFmt:    "table",
			updater:      &fakeEgressPolicyUpdater{err: status.Error(codes.NotFound, "EgressPolicy not found"), actorErr: status.Error(codes.NotFound, "Actor team-a/c1 not found")},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErr:      `actor "c1" in atespace "team-a" not found`,
		},
		{
			name:         "existing actor with no policy points at create",
			outputFmt:    "table",
			updater:      &fakeEgressPolicyUpdater{err: status.Error(codes.NotFound, "EgressPolicy not found")},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErr:      `actor "c1" in atespace "team-a" has no egress policy to update; create it with "kubectl ate create egress-policy"`,
		},
		{
			name:         "actor lookup error wraps",
			outputFmt:    "table",
			updater:      &fakeEgressPolicyUpdater{err: status.Error(codes.NotFound, "EgressPolicy not found"), actorErr: status.Error(codes.PermissionDenied, "denied")},
			wantActorReq: &ateapipb.GetActorRequest{Actor: actor},
			wantErr:      `failed to get actor "c1" in atespace "team-a": rpc error: code = PermissionDenied desc = denied`,
		},
		{
			name:      "other error wraps",
			outputFmt: "table",
			updater:   &fakeEgressPolicyUpdater{err: status.Error(codes.Unavailable, "api-server down")},
			wantErr:   `failed to update egress policy for actor "c1" in atespace "team-a": rpc error: code = Unavailable desc = api-server down`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			runner := &updateEgressPolicyRunner{
				updater:   test.updater,
				guard:     &fakeEgressPolicyGuard{version: ateapipb.EgressPolicyContractVersion},
				actor:     actor,
				policy:    manifest,
				outputFmt: test.outputFmt,
				stdout:    &stdout,
			}
			err := runner.Run(context.Background())
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != test.wantErr {
				t.Fatalf("Run() error = %q, want %q", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(wantReq, test.updater.req, protocmp.Transform()); diff != "" {
				t.Errorf("request mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantActorReq, test.updater.actorReq, protocmp.Transform()); diff != "" {
				t.Errorf("actor request mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantOut, stdout.String()); diff != "" {
				t.Errorf("stdout mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fakeEgressPolicyGuard reports a configured egress policy contract, or
// contractErr, and answers the read-back with stored. readBack records whether
// the runner read the policy back.
type fakeEgressPolicyGuard struct {
	version     string
	contractErr error
	stored      *ateapipb.EgressPolicy
	storedErr   error
	readBack    bool
}

func (f *fakeEgressPolicyGuard) GetEgressPolicyContract(ctx context.Context, req *ateapipb.GetEgressPolicyContractRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicyContract, error) {
	if f.contractErr != nil {
		return nil, f.contractErr
	}
	return &ateapipb.EgressPolicyContract{Version: f.version}, nil
}

func (f *fakeEgressPolicyGuard) GetActorEgressPolicy(ctx context.Context, req *ateapipb.GetActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.readBack = true
	if f.storedErr != nil {
		return nil, f.storedErr
	}
	return f.stored, nil
}

func TestCheckEgressPolicyContract(t *testing.T) {
	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	tests := []struct {
		name         string
		guard        *fakeEgressPolicyGuard
		wantReported bool
		wantErr      string
	}{
		{
			name:         "same contract passes",
			guard:        &fakeEgressPolicyGuard{version: ateapipb.EgressPolicyContractVersion},
			wantReported: true,
		},
		{
			name:         "another contract is refused with both named",
			guard:        &fakeEgressPolicyGuard{version: "v0.2.0-beta5"},
			wantReported: true,
			wantErr:      `refusing to create the egress policy for actor "c1" in atespace "team-a": ate-api speaks egress-policy contract "v0.2.0-beta5", this kubectl-ate speaks "` + ateapipb.EgressPolicyContractVersion + `", and the two give the rules' fields another meaning; use the kubectl-ate of the server's release`,
		},
		{
			name:         "an empty contract is another contract",
			guard:        &fakeEgressPolicyGuard{},
			wantReported: true,
			wantErr:      `refusing to create the egress policy for actor "c1" in atespace "team-a": ate-api speaks egress-policy contract "", this kubectl-ate speaks "` + ateapipb.EgressPolicyContractVersion + `", and the two give the rules' fields another meaning; use the kubectl-ate of the server's release`,
		},
		{
			name:  "a server without the RPC reports none",
			guard: &fakeEgressPolicyGuard{contractErr: status.Error(codes.Unimplemented, "unknown method GetEgressPolicyContract")},
		},
		{
			name:    "other errors wrap",
			guard:   &fakeEgressPolicyGuard{contractErr: status.Error(codes.Unavailable, "api-server down")},
			wantErr: `failed to read the egress-policy contract of ate-api: rpc error: code = Unavailable desc = api-server down`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reported, err := checkEgressPolicyContract(context.Background(), test.guard, "create", actor)
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != test.wantErr {
				t.Fatalf("checkEgressPolicyContract() error = %q, want %q", gotErr, test.wantErr)
			}
			if reported != test.wantReported {
				t.Errorf("checkEgressPolicyContract() reported = %v, want %v", reported, test.wantReported)
			}
		})
	}
}

// TestEgressPolicyRunners_Guard drives create and update against servers of
// another contract: one that reports it, refused before the write, and one
// that reports none, whose stored rules are read back. A 1.3 ate-api decodes a
// tls_passthrough rule as all and stores it without the hostnames it could not
// decode, which reads back as an empty tls_passthrough rule.
func TestEgressPolicyRunners_Guard(t *testing.T) {
	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	sent := []*ateapipb.EgressRule{{TlsPassthrough: &ateapipb.TLSPassthroughRule{Hostnames: []string{"db.example.com"}}}}
	meta := &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Uid: "3f2b1c0e-8d5a-4b6e-9c1d-2a7e4f6b8c0d", Version: 1}
	manifest := &ateapipb.EgressPolicy{Metadata: meta, Rules: sent}
	accepted := &ateapipb.EgressPolicy{Metadata: meta, Rules: sent}
	asAll := &ateapipb.EgressPolicy{Metadata: meta, Rules: []*ateapipb.EgressRule{{TlsPassthrough: &ateapipb.TLSPassthroughRule{}}}}
	unimplemented := status.Error(codes.Unimplemented, "unknown method GetEgressPolicyContract")

	tests := []struct {
		name         string
		guard        *fakeEgressPolicyGuard
		wantWrite    bool
		wantReadBack bool
		wantErr      []string
	}{
		{
			name:    "a server of another contract is refused before the write",
			guard:   &fakeEgressPolicyGuard{version: "v0.2.0-beta5"},
			wantErr: []string{`ate-api speaks egress-policy contract "v0.2.0-beta5", this kubectl-ate speaks "` + ateapipb.EgressPolicyContractVersion + `"`},
		},
		{
			name:      "a server of the same contract is not read back",
			guard:     &fakeEgressPolicyGuard{version: ateapipb.EgressPolicyContractVersion},
			wantWrite: true,
		},
		{
			name:         "a server without a contract that stored the rules passes",
			guard:        &fakeEgressPolicyGuard{contractErr: unimplemented, stored: accepted},
			wantWrite:    true,
			wantReadBack: true,
		},
		{
			name:         "a server without a contract that stored other rules is refused with the difference",
			guard:        &fakeEgressPolicyGuard{contractErr: unimplemented, stored: asAll},
			wantWrite:    true,
			wantReadBack: true,
			wantErr: []string{
				`ate-api reports no egress-policy contract and stored the egress policy for actor "c1" in atespace "team-a" with other rules than it accepted`,
				`The stored policy is in force: replace it with the kubectl-ate of the server's release ("kubectl ate get egress-policy c1 -a team-a -o yaml", rewrite the rules in that release's shape, "kubectl ate update egress-policy c1 -a team-a -f <manifest>")`,
				`Rules (-accepted +stored):`,
				`db.example.com`,
			},
		},
		{
			name:         "a server without a contract that changed the policy again is not judged",
			guard:        &fakeEgressPolicyGuard{contractErr: unimplemented, stored: &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Version: 2}}},
			wantWrite:    true,
			wantReadBack: true,
			wantErr:      []string{`changed again before it could be verified (version 1 written, 2 stored)`},
		},
		{
			name:         "a failed read-back is an error",
			guard:        &fakeEgressPolicyGuard{contractErr: unimplemented, storedErr: status.Error(codes.Unavailable, "api-server down")},
			wantWrite:    true,
			wantReadBack: true,
			wantErr:      []string{`reading back the egress policy for actor "c1" in atespace "team-a" to verify it failed`},
		},
	}
	for _, test := range tests {
		for _, verb := range []string{"create", "update"} {
			t.Run(verb+"/"+test.name, func(t *testing.T) {
				guard := *test.guard
				var stdout bytes.Buffer
				var runner interface{ Run(context.Context) error }
				var wrote func() bool
				switch verb {
				case "create":
					creator := &fakeEgressPolicyCreator{policy: accepted}
					runner = &createEgressPolicyRunner{creator: creator, guard: &guard, actor: actor, policy: manifest, outputFmt: "table", stdout: &stdout}
					wrote = func() bool { return creator.req != nil }
				case "update":
					updater := &fakeEgressPolicyUpdater{policy: accepted}
					runner = &updateEgressPolicyRunner{updater: updater, guard: &guard, actor: actor, policy: manifest, outputFmt: "table", stdout: &stdout}
					wrote = func() bool { return updater.req != nil }
				}
				err := runner.Run(context.Background())
				gotErr := ""
				if err != nil {
					gotErr = err.Error()
				}
				if (gotErr == "") != (len(test.wantErr) == 0) {
					t.Fatalf("Run() error = %q, want one containing %q", gotErr, test.wantErr)
				}
				for _, want := range test.wantErr {
					if !strings.Contains(gotErr, want) {
						t.Errorf("Run() error = %q, want it to contain %q", gotErr, want)
					}
				}
				if wrote() != test.wantWrite {
					t.Errorf("wrote = %v, want %v", wrote(), test.wantWrite)
				}
				if guard.readBack != test.wantReadBack {
					t.Errorf("read back = %v, want %v", guard.readBack, test.wantReadBack)
				}
				if (err == nil) != (stdout.Len() > 0) {
					t.Errorf("stdout = %q with error %v: the policy prints only when the write is accepted", stdout.String(), err)
				}
			})
		}
	}
}

func TestRequireEgressPolicyPreconditions(t *testing.T) {
	t.Parallel()

	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "c1"}
	const wantErr = `manifest for actor "c1" in atespace "team-a" lacks the metadata.uid and metadata.version preconditions that "get egress-policy -o yaml" prints`

	tests := []struct {
		name     string
		metadata *ateapipb.ResourceMetadata
		wantErr  string
	}{
		{name: "uid and version present", metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Uid: "u", Version: 1}},
		{name: "uid missing", metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Version: 1}, wantErr: wantErr},
		{name: "version missing", metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default", Uid: "u"}, wantErr: wantErr},
		{name: "both missing", metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, wantErr: wantErr},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := requireEgressPolicyPreconditions(&ateapipb.EgressPolicy{Metadata: test.metadata}, actor)
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != test.wantErr {
				t.Errorf("requireEgressPolicyPreconditions() error = %q, want %q", gotErr, test.wantErr)
			}
		})
	}
}
