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
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	require.NoError(t, err)
	return b
}

func appendString(b []byte, num protowire.Number, s string) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func appendMessage(b []byte, num protowire.Number, m []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, m)
}

func TestDecodeActorTemplateMigratesTrustBundleName(t *testing.T) {
	// A projection written before the repeated names: field 1 holds its name.
	legacy := appendString(appendString(nil, legacyTrustBundleDataSourceName, "custom-ca"), 2, "ca.pem")
	// One whose field 1 a later update dropped.
	dropped := appendString(nil, 2, "ca.pem")
	named := mustMarshal(t, &forkpb.TrustBundleDataSource{Names: []string{"a", "b"}, Path: "ca.pem"})
	for _, tc := range []struct {
		name  string
		tb    []byte
		names []string
	}{
		{"legacy name", legacy, []string{"custom-ca"}},
		{"legacy name dropped", dropped, []string{legacyTrustBundleName}},
		{"names kept", named, []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := appendMessage(nil, 2, tc.tb)
			si := appendMessage(nil, 1, ds)
			vol := appendMessage(appendString(nil, 1, "info"), 5, si)
			row := appendMessage(nil, 4, vol)
			tmpl, err := decodeActorTemplate(row)
			require.NoError(t, err)
			tb := tmpl.GetVolumes()[0].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
			require.Equal(t, tc.names, tb.GetNames())
			require.Equal(t, "ca.pem", tb.GetPath())
			require.False(t, hasUnknown(tmpl.ProtoReflect()))
		})
	}
}

func TestDecodeActorTemplateDefaultsOnCommit(t *testing.T) {
	tmpl, err := decodeActorTemplate(mustMarshal(t, &forkpb.ActorTemplate{SnapshotConfig: &forkpb.SnapshotConfig{StorageLocation: location}}))
	require.NoError(t, err)
	require.Equal(t, forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, tmpl.GetSnapshotConfig().GetOnCommit())
}

func TestDecodeActorMigratesLegacySnapshots(t *testing.T) {
	t.Run("1.4 source local snapshot name in field 4", func(t *testing.T) {
		es := appendString(mustMarshal(t, &forkpb.ExternalSnapshot{SnapshotUri: actorURI(pauseName), ActorTemplateUid: templateUID}), legacyExternalSnapshotSourceLocalSnapshotName, pauseName)
		st := appendMessage(mustMarshal(t, &forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED}), 4, es)
		a, err := decodeActor(appendMessage(nil, 7, st))
		require.NoError(t, err)
		require.Equal(t, pauseName, a.GetStatus().GetExternalSnapshot().GetSourceLocalSnapshotName())
		require.Equal(t, templateUID, a.GetStatus().GetExternalSnapshot().GetActorTemplateUid())
		require.False(t, hasUnknown(a.ProtoReflect()))
	})
	t.Run("1.3 source name in field 3 and template on the status", func(t *testing.T) {
		es := mustMarshal(t, &forkpb.ExternalSnapshot{SnapshotUri: actorURI(pauseName), ActorTemplateUid: pauseName})
		st := appendMessage(mustMarshal(t, &forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED}), 4, es)
		st = appendString(st, legacyActorStatusBuiltOnTemplateUID, templateUID)
		a, err := decodeActor(appendMessage(nil, 7, st))
		require.NoError(t, err)
		got := a.GetStatus().GetExternalSnapshot()
		require.Equal(t, pauseName, got.GetSourceLocalSnapshotName())
		require.Equal(t, templateUID, got.GetActorTemplateUid())
	})
	t.Run("1.8 local snapshot node and scope", func(t *testing.T) {
		ls := appendString(mustMarshal(t, &forkpb.LocalSnapshot{SnapshotName: pauseName}), legacyLocalSnapshotNodes, "node-1")
		ls = protowire.AppendVarint(protowire.AppendTag(ls, legacyLocalSnapshotContentScope, protowire.VarintType), uint64(forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA))
		st := appendMessage(mustMarshal(t, &forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_PAUSED}), 5, ls)
		a, err := decodeActor(appendMessage(nil, 7, st))
		require.NoError(t, err)
		require.Equal(t, "node-1", a.GetStatus().GetLocalSnapshot().GetNodeName())
		require.Equal(t, forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA, a.GetStatus().GetLocalSnapshot().GetContentScope())
		require.Equal(t, "node-1", a.GetStatus().GetAssignedNode())
	})
	t.Run("stale crash on a suspended actor", func(t *testing.T) {
		a, err := decodeActor(mustMarshal(t, &forkpb.Actor{Status: &forkpb.ActorStatus{State: forkpb.ActorState_ACTOR_STATE_SUSPENDED, Crash: &forkpb.ActorCrash{Message: "old"}}}))
		require.NoError(t, err)
		require.Nil(t, a.GetStatus().GetCrash())
	})
}
