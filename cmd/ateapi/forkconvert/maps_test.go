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

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	ateapi "go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	actorUID    = "11111111-1111-4111-8111-111111111111"
	tagUID      = "22222222-2222-4222-8222-222222222222"
	templateUID = "33333333-3333-4333-8333-333333333333"
	snapName    = "44444444-4444-4444-8444-444444444444"
	pauseName   = "55555555-5555-4555-8555-555555555555"
	location    = "s3://ate-snapshots/kagent"
)

func actorURI(name string) string {
	return location + "/atespaces/team/actors/" + actorUID + "/snapshots/" + name
}

func tagURI() string { return location + "/atespaces/team/tags/" + tagUID }

func requireProtoEqual(t *testing.T, want, got any) {
	t.Helper()
	require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
}

func forkTemplate() *forkpb.ActorTemplate {
	return &forkpb.ActorTemplate{
		Metadata:       &forkpb.ResourceMetadata{Atespace: "team", Name: "agent", Uid: templateUID, Version: 3, CreateTime: timestamppb.Now()},
		WorkerSelector: &forkpb.Selector{MatchLabels: map[string]string{"pool": "a"}},
		Containers: []*forkpb.Container{{
			Name:            "kagent",
			Image:           "registry/agent@sha256:abc",
			Command:         []string{"/bin/agent"},
			Args:            []string{"--serve"},
			Env:             []*forkpb.EnvVar{{Name: "A", Value: "b"}},
			WakeupProbe:     &forkpb.ContainerWakeupProbe{HttpGet: &forkpb.HTTPGetAction{Path: "/ready", Port: 8080}, TimeoutSeconds: 30},
			VolumeMounts:    []*forkpb.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "shared", MountPath: "/shared", SubPath: "team", ReadOnly: true}},
			SecurityContext: &forkpb.SecurityContext{Capabilities: &forkpb.Capabilities{Drop: []string{"ALL"}}},
			Resources:       &forkpb.Resources{Limits: []*forkpb.Limits{{Name: "cpu", Quantity: "500m"}, {Name: "memory", Quantity: "1Gi"}}},
		}},
		Volumes: []*forkpb.Volume{
			{Name: "data", DurableDir: &forkpb.DurableDirVolumeSource{}},
			{Name: "scratch", ExternalVolumeTemplate: &forkpb.ExternalVolumeTemplate{Capacity: "10Gi", StorageClassName: "fast"}},
			{Name: "info", SystemInfo: &forkpb.SystemInfoVolumeSource{DataSources: []*forkpb.SystemInfoDataSource{
				{ActorMetadata: &forkpb.ActorMetadataDataSource{Items: []*forkpb.ActorMetadataItem{{Field: forkpb.ActorMetadataField_ACTOR_METADATA_FIELD_UID, Path: "uid"}}}},
				{TrustBundle: &forkpb.TrustBundleDataSource{Names: []string{"egress-mitm.ate.dev"}, Path: "ca.pem"}},
			}}},
			{Name: "tools", Image: &forkpb.ImageVolumeSource{Reference: "registry/tools:1"}},
			{Name: "shared", ExistingVolume: &forkpb.ExistingVolumeSource{}},
		},
		SnapshotConfig: &forkpb.SnapshotConfig{OnCommit: forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA, StorageLocation: location},
		SandboxConfig:  &forkpb.SandboxConfig{SandboxClass: forkpb.SandboxClass_SANDBOX_CLASS_GVISOR, ConfigName: "gvisor-default"},
		Resources:      &forkpb.Resources{Limits: []*forkpb.Limits{{Name: "cpu", Quantity: "2"}, {Name: "memory", Quantity: "512Mi"}}},
		Status: &forkpb.ActorTemplateStatus{GoldenSnapshotStatus: &forkpb.GoldenSnapshotStatus{
			GoldenTag: &forkpb.ObjectRef{Atespace: "ate-golden", Name: templateUID}, SnapshotLayout: 2,
		}},
		DefaultEgressPolicy: &forkpb.EgressPolicyTemplate{Rules: []*forkpb.EgressRule{{
			Tcp: &forkpb.TCPRule{Hostnames: []string{"db.example"}, Ports: &forkpb.Ports{Numbers: []int32{5432}}},
		}, {
			Https: &forkpb.HTTPSRule{Hostnames: []string{"api.example"}, Effects: &forkpb.HttpRuleEffects{ReplaceHeaders: []*forkpb.CredentialHeader{{Header: "Authorization", CredentialUri: "secret://x", PathPrefix: "/v1"}}}},
		}}},
	}
}

func TestConvertActorTemplate(t *testing.T) {
	f := forkTemplate()
	got, err := convertActorTemplate(f)
	require.NoError(t, err)
	want := &ateapi.ActorTemplate{
		Metadata:       &ateapi.ResourceMetadata{Atespace: "team", Name: "agent", Uid: templateUID, Version: 3, CreateTime: f.GetMetadata().GetCreateTime()},
		WorkerSelector: &ateapi.Selector{MatchLabels: map[string]string{"pool": "a"}},
		Containers: []*ateapi.Container{{
			Name:            "kagent",
			Image:           "registry/agent@sha256:abc",
			Command:         []string{"/bin/agent"},
			Args:            []string{"--serve"},
			Env:             []*ateapi.EnvVar{{Name: "A", Value: "b"}},
			WakeupProbe:     &ateapi.WakeupProbe{HttpGet: &ateapi.HttpGetAction{Path: "/ready", Port: 8080}, TimeoutSeconds: 30},
			VolumeMounts:    []*ateapi.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "shared", MountPath: "/shared", SubPath: "team", ReadOnly: true}},
			SecurityContext: &ateapi.SecurityContext{Capabilities: &ateapi.Capabilities{Drop: []string{"ALL"}}},
			Resources:       &ateapi.ResourceRequirements{Limits: &ateapi.Resources{CpuMillicores: 500, MemoryBytes: 1 << 30}},
		}},
		Volumes: []*ateapi.Volume{
			{Name: "data", DurableDir: &ateapi.DurableDirVolumeSource{}},
			{Name: "scratch", External: &ateapi.ExternalVolumeSource{Template: &ateapi.ExternalVolumeTemplate{CapacityBytes: 10 << 30, StorageClassName: "fast"}}},
			{Name: "info", SystemInfo: &ateapi.SystemInfoVolumeSource{DataSources: []*ateapi.SystemInfoDataSource{
				{ActorMetadata: &ateapi.ActorMetadataDataSource{Items: []*ateapi.ActorMetadataItem{{Field: ateapi.ActorMetadataField_ACTOR_METADATA_FIELD_UID, Path: "uid"}}}},
				{TrustBundle: &ateapi.TrustBundleDataSource{Names: []string{"egress-mitm.ate.dev"}, Path: "ca.pem"}},
			}}},
			{Name: "tools", Image: &ateapi.ImageVolumeSource{Reference: "registry/tools:1"}},
			{Name: "shared", ExistingVolume: &ateapi.ExistingVolumeSource{}},
		},
		SnapshotConfig: &ateapi.SnapshotConfig{PreferredFidelity: ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, StorageLocation: location},
		Resources:      &ateapi.ResourceRequirements{Limits: &ateapi.Resources{CpuMillicores: 2000, MemoryBytes: 512 << 20}},
		GoldenEgressPolicy: &ateapi.EgressPolicyTemplate{Rules: []*ateapi.EgressRule{{
			Tcp: &ateapi.TcpRule{Hostnames: []string{"db.example"}, Ports: &ateapi.Ports{Numbers: []int32{5432}}},
		}, {
			Https: &ateapi.HttpsRule{Hostnames: []string{"api.example"}, Effects: &ateapi.HttpRuleEffects{ReplaceHeaders: []*ateapi.CredentialHeader{{Header: "Authorization", CredentialUri: "secret://x", PathPrefix: "/v1"}}}},
		}}},
	}
	requireProtoEqual(t, want, got)
}

func TestPreferredFidelity(t *testing.T) {
	for _, tc := range []struct {
		onCommit forkpb.SnapshotContentScope
		want     ateapi.SnapshotFidelity
	}{
		{forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY},
		{forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA, ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES},
	} {
		t.Run(tc.onCommit.String(), func(t *testing.T) {
			require.Equal(t, tc.want, preferredFidelity(tc.onCommit))
		})
	}
}

func TestConvertResources(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limits  []*forkpb.Limits
		want    *ateapi.Resources
		wantErr string
	}{
		{"cpu and memory", []*forkpb.Limits{{Name: "cpu", Quantity: "1500m"}, {Name: "memory", Quantity: "256Mi"}}, &ateapi.Resources{CpuMillicores: 1500, MemoryBytes: 256 << 20}, ""},
		{"whole cpus", []*forkpb.Limits{{Name: "cpu", Quantity: "3"}}, &ateapi.Resources{CpuMillicores: 3000}, ""},
		{"decimal memory", []*forkpb.Limits{{Name: "memory", Quantity: "1G"}}, &ateapi.Resources{MemoryBytes: 1_000_000_000}, ""},
		{"other resource", []*forkpb.Limits{{Name: "nvidia.com/gpu", Quantity: "1"}}, nil, `"nvidia.com/gpu" has no field`},
		{"bad quantity", []*forkpb.Limits{{Name: "cpu", Quantity: "lots"}}, nil, `limit "cpu"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertResources(&forkpb.Resources{Limits: tc.limits})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			requireProtoEqual(t, &ateapi.ResourceRequirements{Limits: tc.want}, got)
		})
	}
}

func TestConvertTag(t *testing.T) {
	meta := &forkpb.ResourceMetadata{Atespace: "team", Name: "release", Uid: tagUID, Version: 2}
	for _, tc := range []struct {
		name    string
		tag     *forkpb.Tag
		want    *ateapi.Tag
		wantErr string
	}{{
		name: "with snapshot",
		tag: &forkpb.Tag{Metadata: meta, Scope: forkpb.TagScope_TAG_SCOPE_PUBLISHED, SourceActor: &forkpb.ObjectRef{Atespace: "team", Name: "a"},
			Status: &forkpb.TagStatus{ActorTemplateUid: templateUID, StorageLocation: location, Snapshot: &forkpb.ExternalSnapshot{
				SnapshotUri: tagURI(), ContentScope: forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, ActorTemplateUid: templateUID, SourceLocalSnapshotName: pauseName,
			}}},
		want: &ateapi.Tag{Metadata: &ateapi.ResourceMetadata{Atespace: "team", Name: "release", Uid: tagUID, Version: 2}, Scope: ateapi.TagScope_TAG_SCOPE_PUBLISHED, SourceActor: &ateapi.ObjectRef{Atespace: "team", Name: "a"},
			Status: &ateapi.TagStatus{ActorTemplateUid: templateUID, Snapshot: &ateapi.Snapshot{
				Uuid: tagUID, Generation: 1, Owner: ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG, Fidelity: ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: templateUID,
				DurableSnapshot: &ateapi.SnapshotStorage{State: ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapi.ObjectSnapshot{SnapshotUri: tagURI()}},
				SandboxRuntime:  &ateapi.ActorSandboxRuntime{SandboxClass: "gvisor"},
			}}},
	}, {
		name:    "snapshot under an actor prefix",
		tag:     &forkpb.Tag{Metadata: meta, Status: &forkpb.TagStatus{Snapshot: &forkpb.ExternalSnapshot{SnapshotUri: actorURI(snapName)}}},
		wantErr: "not under the tag's own prefix",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertTag(tc.tag, "gvisor")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			requireProtoEqual(t, tc.want, got)
		})
	}
}

func TestConvertActor(t *testing.T) {
	meta := &forkpb.ResourceMetadata{Atespace: "team", Name: "session", Uid: actorUID, Version: 9}
	tmplRef := &forkpb.ObjectRef{Atespace: "team", Name: "agent"}
	external := &forkpb.ExternalSnapshot{SnapshotUri: actorURI(snapName), ContentScope: forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, ActorTemplateUid: templateUID}
	pauseCopy := &forkpb.ExternalSnapshot{SnapshotUri: actorURI(pauseName), ContentScope: forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA, ActorTemplateUid: templateUID, SourceLocalSnapshotName: pauseName}
	snapshot := func(uri, uuid string, owner ateapi.SnapshotOwner) []*ateapi.Snapshot {
		return []*ateapi.Snapshot{{
			Uuid: uuid, Generation: 1, Owner: owner, Fidelity: ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: templateUID,
			DurableSnapshot: &ateapi.SnapshotStorage{State: ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapi.ObjectSnapshot{SnapshotUri: uri}},
			SandboxRuntime:  &ateapi.ActorSandboxRuntime{SandboxClass: "gvisor"},
		}}
	}
	actor := func(st *forkpb.ActorStatus) *forkpb.Actor {
		return &forkpb.Actor{Metadata: meta, ActorTemplate: tmplRef, Status: st}
	}
	wantActor := func(st *ateapi.ActorStatus) *ateapi.Actor {
		return &ateapi.Actor{Metadata: &ateapi.ResourceMetadata{Atespace: "team", Name: "session", Uid: actorUID, Version: 9}, ActorTemplate: &ateapi.ObjectRef{Atespace: "team", Name: "agent"}, Status: st}
	}
	crashTime := timestamppb.Now()
	for _, tc := range []struct {
		name    string
		actor   *forkpb.Actor
		want    *ateapi.Actor
		wantErr string
	}{{
		name: "suspended keeps its snapshot, fencing token and volumes",
		actor: &forkpb.Actor{
			Metadata: meta, ActorTemplate: tmplRef,
			WorkerSelector:  &forkpb.Selector{MatchLabels: map[string]string{"zone": "a"}},
			SourceTag:       &forkpb.ObjectRef{Atespace: "team", Name: "release"},
			ExistingVolumes: []*forkpb.ExistingVolume{{Name: "shared", Driver: "nfs.csi.k8s.io", VolumeHandle: "h", AccessMode: forkpb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY, SubPath: "x"}},
			Status: &forkpb.ActorStatus{
				State: forkpb.ActorState_ACTOR_STATE_SUSPENDED, ExternalSnapshot: external,
				ActorVolumes:  []*forkpb.ExternalVolume{{VolumeName: "scratch", StorageVolumeId: "vol-1", VolumeType: "ebs.csi.aws.com", Status: forkpb.ExternalVolume_STATUS_CREATED, VolumeContext: map[string]string{"k": "v"}}},
				FencingToken:  &forkpb.FencingToken{Holder: "kagent", Generation: 4},
				SandboxAssets: []*forkpb.SandboxAsset{{Name: "gvisor", Url: "gs://x", Sha256: "y"}},
				AssignedNode:  "node-1",
			},
		},
		want: &ateapi.Actor{
			Metadata: &ateapi.ResourceMetadata{Atespace: "team", Name: "session", Uid: actorUID, Version: 9}, ActorTemplate: &ateapi.ObjectRef{Atespace: "team", Name: "agent"},
			WorkerSelector:  &ateapi.Selector{MatchLabels: map[string]string{"zone": "a"}},
			SourceTag:       &ateapi.ObjectRef{Atespace: "team", Name: "release"},
			ExistingVolumes: []*ateapi.ExistingVolume{{Name: "shared", Driver: "nfs.csi.k8s.io", VolumeHandle: "h", AccessMode: ateapi.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY, SubPath: "x"}},
			Status: &ateapi.ActorStatus{
				State:                  ateapi.ActorState_ACTOR_STATE_SUSPENDED,
				ExternalVolumes:        &ateapi.ExternalVolumes{Volumes: []*ateapi.ExternalVolumeStatus{{Name: "scratch", StorageVolumeId: "vol-1", VolumeType: "ebs.csi.aws.com", VolumeContext: map[string]string{"k": "v"}}}},
				FencingToken:           &ateapi.FencingToken{Holder: "kagent", Generation: 4},
				Snapshots:              snapshot(actorURI(snapName), snapName, ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR),
				LastAssignedGeneration: 1,
			},
		},
	}, {
		name:  "suspended without a snapshot",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED}),
		want:  wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED}),
	}, {
		name:  "borrowing a tag snapshot",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED, ExternalSnapshot: &forkpb.ExternalSnapshot{SnapshotUri: tagURI(), ActorTemplateUid: templateUID}}),
		want:  wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED, Snapshots: snapshot(tagURI(), tagUID, ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG), LastAssignedGeneration: 1}),
	}, {
		name:  "crashed keeps its crash",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_CRASHED, ExternalSnapshot: external, Crash: &forkpb.ActorCrash{Message: "oom", CrashTime: crashTime}}),
		want: wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_CRASHED, Crash: &ateapi.ActorCrash{Message: "oom", CrashTime: crashTime},
			Snapshots: snapshot(actorURI(snapName), snapName, ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR), LastAssignedGeneration: 1}),
	}, {
		name:  "crashed on missing trust bundle names returns to suspended",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_CRASHED, ExternalSnapshot: external, Crash: &forkpb.ActorCrash{Message: "atelet: no trust bundle names given"}}),
		want:  wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED, Snapshots: snapshot(actorURI(snapName), snapName, ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR), LastAssignedGeneration: 1}),
	}, {
		name: "paused resumes from its pause copy",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_PAUSED, ExternalSnapshot: external,
			LocalSnapshot: &forkpb.LocalSnapshot{SnapshotName: pauseName, DurableCopy: pauseCopy, NodeName: "node-1"}}),
		want: wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_SUSPENDED, Snapshots: snapshot(actorURI(pauseName), pauseName, ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR), LastAssignedGeneration: 1}),
	}, {
		name: "paused before its upload with an older snapshot",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_PAUSED, ExternalSnapshot: external,
			LocalSnapshot: &forkpb.LocalSnapshot{SnapshotName: pauseName, NodeName: "node-1"}}),
		wantErr: "pause that was not uploaded",
	}, {
		name:  "paused with no durable state",
		actor: actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_PAUSED, LocalSnapshot: &forkpb.LocalSnapshot{SnapshotName: pauseName}}),
		want:  wantActor(&ateapi.ActorStatus{State: ateapi.ActorState_ACTOR_STATE_CRASHED, Crash: &ateapi.ActorCrash{Message: noDurableSnapshotCrash}}),
	}, {
		name:    "running",
		actor:   actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_RUNNING}),
		wantErr: "state ACTOR_STATE_RUNNING",
	}, {
		name:    "pending external volume",
		actor:   actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED, ActorVolumes: []*forkpb.ExternalVolume{{VolumeName: "scratch", Status: forkpb.ExternalVolume_STATUS_PENDING}}}),
		wantErr: `external volume "scratch" is STATUS_PENDING`,
	}, {
		name:    "snapshot of another actor",
		actor:   actor(&forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED, ExternalSnapshot: &forkpb.ExternalSnapshot{SnapshotUri: location + "/atespaces/team/actors/" + tagUID + "/snapshots/" + snapName}}),
		wantErr: "another actor's prefix",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertActor(tc.actor, "gvisor")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			requireProtoEqual(t, tc.want, got)
			require.False(t, hasUnknown(got.ProtoReflect()))
		})
	}
}

func TestRecodeRefusesUndeclaredFields(t *testing.T) {
	src := &forkpb.ExternalSnapshot{SnapshotUri: tagURI(), SourceLocalSnapshotName: pauseName}
	err := recode(src, &ateapi.ObjectSnapshot{})
	require.ErrorContains(t, err, "does not declare")
}

func TestSameBytes(t *testing.T) {
	policy := &forkpb.EgressPolicy{
		Metadata: &forkpb.ResourceMetadata{Atespace: "team", Name: "session", Uid: actorUID, Version: 1},
		Rules: []*forkpb.EgressRule{{
			Tcp:  &forkpb.TCPRule{Cidrs: []string{"10.0.0.0/8"}, Ports: &forkpb.Ports{All: &forkpb.AllPorts{}}},
			Http: &forkpb.HTTPRule{Hostnames: []string{"h"}, Effects: &forkpb.HttpRuleEffects{ReplaceHeaders: []*forkpb.CredentialHeader{{Header: "X", PathPrefix: "/p", ActorJwt: &forkpb.ActorJWTSource{Audiences: []string{"a"}}}}}},
		}},
	}
	require.NoError(t, sameBytes(mustMarshal(t, policy), &forkpb.EgressPolicy{}, &ateapi.EgressPolicy{}))

	tmpl := mustMarshal(t, &forkpb.ActorTemplate{SandboxConfig: &forkpb.SandboxConfig{ConfigName: "x"}})
	require.ErrorContains(t, sameBytes(tmpl, &forkpb.ActorTemplate{}, &ateapi.ActorTemplate{}), "does not declare")
}
