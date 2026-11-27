// Copyright 2026 The Agent Substrate Authors
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

package forkconvert

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"google.golang.org/protobuf/proto"
)

func row(t *testing.T, atespace, name, uid string, m proto.Message) Row {
	return Row{Atespace: atespace, Name: name, UID: uid, Version: 1, Proto: mustMarshal(t, m)}
}

func forkActor(name, uid string, state forkpb.ActorState, uri string) *forkpb.Actor {
	a := &forkpb.Actor{
		Metadata:      &forkpb.ResourceMetadata{Atespace: "team", Name: name, Uid: uid, Version: 1},
		ActorTemplate: &forkpb.ObjectRef{Atespace: "team", Name: "agent"},
		Status:        &forkpb.ActorStatus{State: state},
	}
	if uri != "" {
		a.Status.ExternalSnapshot = &forkpb.ExternalSnapshot{SnapshotUri: uri, ActorTemplateUid: templateUID}
	}
	return a
}

func testSource(t *testing.T) *Source {
	tmpl := forkTemplate()
	goldenTag := &forkpb.Tag{Metadata: &forkpb.ResourceMetadata{Atespace: "ate-golden", Name: templateUID, Uid: "66666666-6666-4666-8666-666666666666", Version: 1}}
	userTag := &forkpb.Tag{
		Metadata: &forkpb.ResourceMetadata{Atespace: "team", Name: "release", Uid: tagUID, Version: 1},
		Status:   &forkpb.TagStatus{ActorTemplateUid: templateUID, Snapshot: &forkpb.ExternalSnapshot{SnapshotUri: tagURI(), ActorTemplateUid: templateUID}},
	}
	golden := forkActor(templateUID, "77777777-7777-4777-8777-777777777777", forkpb.ActorState_ACTOR_STATE_SUSPENDED, "")
	golden.Metadata.Atespace = "ate-golden"
	return &Source{
		Atespaces: []Row{
			row(t, "", "team", "a1", &forkpb.Atespace{Metadata: &forkpb.ResourceMetadata{Name: "team", Uid: "a1", Version: 1}}),
			row(t, "", "ate-golden", "a2", &forkpb.Atespace{Metadata: &forkpb.ResourceMetadata{Name: "ate-golden", Uid: "a2", Version: 1}}),
		},
		ActorTemplates: []Row{row(t, "team", "agent", templateUID, tmpl)},
		Tags:           []Row{row(t, "ate-golden", templateUID, goldenTag.Metadata.Uid, goldenTag), row(t, "team", "release", tagUID, userTag)},
		Actors: []Row{
			row(t, "team", "session", actorUID, forkActor("session", actorUID, forkpb.ActorState_ACTOR_STATE_SUSPENDED, actorURI(snapName))),
			row(t, "team", "clone", "88888888-8888-4888-8888-888888888888", forkActor("clone", "88888888-8888-4888-8888-888888888888", forkpb.ActorState_ACTOR_STATE_SUSPENDED, tagURI())),
			row(t, "ate-golden", templateUID, golden.Metadata.Uid, golden),
		},
		EgressPolicies: []Row{
			row(t, "team", "session", "e1", &forkpb.EgressPolicy{Metadata: &forkpb.ResourceMetadata{Atespace: "team", Name: "session", Uid: "e1", Version: 1}, Rules: []*forkpb.EgressRule{{Tcp: &forkpb.TCPRule{Hostnames: []string{"db"}, Ports: &forkpb.Ports{Numbers: []int32{5432}}}}}}),
			row(t, "ate-golden", templateUID, "e2", &forkpb.EgressPolicy{Metadata: &forkpb.ResourceMetadata{Atespace: "ate-golden", Name: templateUID, Uid: "e2", Version: 1}}),
		},
		OpenFGA: map[string]*Table{"tuple": {Columns: []string{"store"}, Rows: [][]any{{"s"}}}},
	}
}

func TestBuild(t *testing.T) {
	p := Build(testSource(t))
	require.Empty(t, p.Refusals)
	require.Len(t, p.Atespaces, 2)
	require.Len(t, p.ActorTemplates, 1)
	require.Len(t, p.Tags, 1, "the golden tag is dropped")
	require.Equal(t, "release", p.Tags[0].Name)
	require.Len(t, p.Actors, 2, "the golden actor is dropped")
	require.Len(t, p.EgressPolicies, 1, "the golden actor's policy is dropped")
	require.Equal(t, testSource(t).EgressPolicies[0].Proto, p.EgressPolicies[0].Proto, "egress policies are copied byte for byte")

	layout := []ContainerMounts{{Name: "kagent", Mounts: []Mount{{Volume: "data", Path: "/data"}}}}
	require.Equal(t, []SnapshotJob{
		{URI: actorURI(snapName), Holders: []string{"actor team/session"}, Containers: layout},
		{URI: tagURI(), Holders: []string{"tag team/release", "actor team/clone"}, Containers: layout},
	}, p.Snapshots)
	require.Contains(t, p.Counts, TableCount{Table: "actors", Source: 3, Target: 2})
	require.Contains(t, p.Counts, TableCount{Table: "tuple", Source: 1, Target: 1})
}

func TestBuildRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, s *Source)
		want   []string
	}{{
		name: "actors in transition",
		mutate: func(t *testing.T, s *Source) {
			s.Actors = append(s.Actors,
				row(t, "team", "busy", "b1", forkActor("busy", "b1", forkpb.ActorState_ACTOR_STATE_RUNNING, "")),
				row(t, "team", "pausing", "b2", forkActor("pausing", "b2", forkpb.ActorState_ACTOR_STATE_PAUSING, "")),
				row(t, "team", "gone", "b3", forkActor("gone", "b3", forkpb.ActorState_ACTOR_STATE_DELETING, "")))
		},
		want: []string{
			"actors team/busy: state ACTOR_STATE_RUNNING; only SUSPENDED, CRASHED and PAUSED actors convert, so suspend it on the 1.x release first",
			"actors team/pausing: state ACTOR_STATE_PAUSING; only SUSPENDED, CRASHED and PAUSED actors convert, so suspend it on the 1.x release first",
			"actors team/gone: state ACTOR_STATE_DELETING; only SUSPENDED, CRASHED and PAUSED actors convert, so suspend it on the 1.x release first",
		},
	}, {
		name: "template with a resource the target cannot hold",
		mutate: func(t *testing.T, s *Source) {
			tmpl := forkTemplate()
			tmpl.Resources.Limits = append(tmpl.Resources.Limits, &forkpb.Limits{Name: "nvidia.com/gpu", Quantity: "1"})
			s.ActorTemplates[0] = row(t, "team", "agent", templateUID, tmpl)
		},
		want: []string{
			`actor_templates team/agent: resources: limit "nvidia.com/gpu" has no field in the target API`,
			"actors team/session: its ActorTemplate team/agent is missing or does not convert",
			"actors team/clone: its ActorTemplate team/agent is missing or does not convert",
			"snapshot " + tagURI() + " of [tag team/release]: no template to take its container layout from; delete the holders on the 1.x release",
		},
	}, {
		name: "microVM template",
		mutate: func(t *testing.T, s *Source) {
			tmpl := forkTemplate()
			tmpl.SandboxConfig.SandboxClass = forkpb.SandboxClass_SANDBOX_CLASS_MICROVM
			s.ActorTemplates[0] = row(t, "team", "agent", templateUID, tmpl)
		},
		want: []string{
			"tag team/release: snapshot " + tagUID + " is a microVM snapshot; ate-convert converts gVisor snapshots only",
			"actor team/session: snapshot " + snapName + " is a microVM snapshot; ate-convert converts gVisor snapshots only",
			"actor team/clone: snapshot " + tagUID + " is a microVM snapshot; ate-convert converts gVisor snapshots only",
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSource(t)
			tc.mutate(t, s)
			require.Equal(t, tc.want, Build(s).Refusals)
		})
	}
}
