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

package atepg

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

const (
	legacyTestActorUID    = "0b7f1c52-8d0e-4f3a-9f41-1d2c3b4a5e60"
	legacyTestTemplateUID = "6a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a5b"
	legacyTestLocalName   = "c3d4e5f6-a7b8-4c9d-8e0f-1a2b3c4d5e6f"
	legacyTestSuspendName = "f1e2d3c4-b5a6-4978-8695-a4b3c2d1e0f9"
)

func legacyTestSnapshotURI(t *testing.T, name string) string {
	t.Helper()
	uri, err := resources.NewActorSnapshotURI("gs://bucket/root", "team-a", legacyTestActorUID, name)
	if err != nil {
		t.Fatalf("NewActorSnapshotURI: %v", err)
	}
	return uri.String()
}

// withUnknownString appends a length-delimited field m's descriptor does not
// declare, the way an older release's encoding carries it.
func withUnknownString[M proto.Message](m M, num protowire.Number, v string) M {
	r := m.ProtoReflect()
	b := protowire.AppendTag(r.GetUnknown(), num, protowire.BytesType)
	r.SetUnknown(protowire.AppendString(b, v))
	return m
}

func legacyTestActor(status *ateapipb.ActorStatus) *ateapipb.Actor {
	return &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "actor-a", Uid: legacyTestActorUID},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: "default", Name: "template-a"},
		Status:        status,
	}
}

func TestUnmarshalStoredMigratesLegacySnapshots(t *testing.T) {
	pauseURI := legacyTestSnapshotURI(t, legacyTestLocalName)
	suspendURI := legacyTestSnapshotURI(t, legacyTestSuspendName)
	full := ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL

	// What every release since reads: the copy names its source and the
	// template its guest state was built on.
	migratedCopy := &ateapipb.ExternalSnapshot{
		SnapshotUri:             pauseURI,
		ContentScope:            full,
		ActorTemplateUid:        legacyTestTemplateUID,
		SourceLocalSnapshotName: legacyTestLocalName,
	}

	tests := []struct {
		name    string
		written *ateapipb.Actor
		want    *ateapipb.ActorStatus
	}{{
		// 1.3: field 3 is the pause copy's source, and the template the
		// guest state was built on is ActorStatus field 11.
		name: "1.3 pause copy, adopted by a suspend",
		written: legacyTestActor(withUnknownString(&ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{
				SnapshotUri: pauseURI, ContentScope: full, ActorTemplateUid: legacyTestLocalName,
			},
		}, legacyActorStatusBuiltOnTemplateUID, legacyTestTemplateUID)),
		want: &ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: migratedCopy,
		},
	}, {
		name: "1.3 paused actor with its durable copy",
		written: legacyTestActor(withUnknownString(&ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_PAUSED,
			LocalSnapshot: &ateapipb.LocalSnapshot{
				SnapshotName:              legacyTestLocalName,
				NodeVmsWithLocalSnapshots: []string{"node-a"},
				DurableCopy: &ateapipb.ExternalSnapshot{
					SnapshotUri: pauseURI, ContentScope: full, ActorTemplateUid: legacyTestLocalName,
				},
			},
		}, legacyActorStatusBuiltOnTemplateUID, legacyTestTemplateUID)),
		want: &ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_PAUSED,
			LocalSnapshot: &ateapipb.LocalSnapshot{
				SnapshotName:              legacyTestLocalName,
				NodeVmsWithLocalSnapshots: []string{"node-a"},
				DurableCopy:               migratedCopy,
			},
		},
	}, {
		name: "1.3 checkpoint suspend",
		written: legacyTestActor(withUnknownString(&ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: suspendURI, ContentScope: full},
		}, legacyActorStatusBuiltOnTemplateUID, legacyTestTemplateUID)),
		want: &ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{
				SnapshotUri: suspendURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			},
		},
	}, {
		// 1.4.0 to 1.5.0-rc.1: field 3 is the template, field 4 the source.
		name: "1.4 pause copy",
		written: legacyTestActor(&ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: withUnknownString(&ateapipb.ExternalSnapshot{
				SnapshotUri: pauseURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			}, legacyExternalSnapshotSourceLocalSnapshotName, legacyTestLocalName),
		}),
		want: &ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: migratedCopy,
		},
	}, {
		name: "1.4 checkpoint suspend",
		written: legacyTestActor(&ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{
				SnapshotUri: suspendURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			},
		}),
		want: &ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{
				SnapshotUri: suspendURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			},
		},
	}, {
		name: "current pause copy",
		written: legacyTestActor(&ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: proto.CloneOf(migratedCopy),
		}),
		want: &ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: migratedCopy,
		},
	}, {
		name: "unknown fields of a newer release are dropped",
		written: withUnknownString(legacyTestActor(&ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: withUnknownString(&ateapipb.ExternalSnapshot{
				SnapshotUri: suspendURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			}, 9999, "newer"),
		}), 9999, "newer"),
		want: &ateapipb.ActorStatus{
			State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{
				SnapshotUri: suspendURI, ContentScope: full, ActorTemplateUid: legacyTestTemplateUID,
			},
		},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := proto.Marshal(tc.written)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := &ateapipb.Actor{}
			if err := unmarshalStored(b, got); err != nil {
				t.Fatalf("unmarshalStored: %v", err)
			}
			if diff := cmp.Diff(tc.want, got.GetStatus(), protocmp.Transform()); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
			reencoded, err := proto.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !unknownFieldsFree(t, reencoded) {
				t.Errorf("the decoded actor still holds unknown fields")
			}
		})
	}
}

// unknownFieldsFree reports whether b decodes without a field the current
// descriptors leave unknown, anywhere in the actor.
func unknownFieldsFree(t *testing.T, b []byte) bool {
	t.Helper()
	strict := &ateapipb.Actor{}
	if err := proto.Unmarshal(b, strict); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	discarded := &ateapipb.Actor{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(b, discarded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return proto.Equal(strict, discarded)
}

func TestLegacyPauseCopyMigrationIsPersisted(t *testing.T) {
	s := setupPostgresPersistence(t)
	ctx := context.Background()
	createTestAtespace(t, s, "team-a")
	created, err := s.CreateActor(ctx, legacyTestActor(&ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED}), nil)
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	actorRef := resources.ActorRefFromActor(created)

	// Overwrite the row with what a 1.3 suspend that adopted its pause copy
	// wrote for this actor.
	pauseURI := legacyTestSnapshotURI(t, legacyTestLocalName)
	legacy := proto.CloneOf(created)
	legacy.Status = withUnknownString(&ateapipb.ActorStatus{
		State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
		ExternalSnapshot: &ateapipb.ExternalSnapshot{
			SnapshotUri: pauseURI, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, ActorTemplateUid: legacyTestLocalName,
		},
	}, legacyActorStatusBuiltOnTemplateUID, legacyTestTemplateUID)
	legacyBytes, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actors SET proto = $1 WHERE atespace = $2 AND name = $3`, legacyBytes, actorRef.Atespace, actorRef.Name); err != nil {
		t.Fatalf("writing the 1.3 row: %v", err)
	}

	got, err := s.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if es := got.GetStatus().GetExternalSnapshot(); es.GetSourceLocalSnapshotName() != legacyTestLocalName || es.GetActorTemplateUid() != legacyTestTemplateUID {
		t.Fatalf("GetActor external snapshot = %v, want source %q under template %q", es, legacyTestLocalName, legacyTestTemplateUID)
	}

	if _, err := s.UpdateActor(ctx, actorRef, store.PreconditionFrom(got), func(*ateapipb.Actor) error { return nil }); err != nil {
		t.Fatalf("UpdateActor: %v", err)
	}
	var stored []byte
	if err := s.pool.QueryRow(ctx, `SELECT proto FROM actors WHERE atespace = $1 AND name = $2`, actorRef.Atespace, actorRef.Name).Scan(&stored); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if !unknownFieldsFree(t, stored) {
		t.Errorf("the updated row still holds the 1.3 fields")
	}
	persisted := &ateapipb.Actor{}
	if err := proto.Unmarshal(stored, persisted); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if es := persisted.GetStatus().GetExternalSnapshot(); es.GetSourceLocalSnapshotName() != legacyTestLocalName || es.GetActorTemplateUid() != legacyTestTemplateUID {
		t.Errorf("persisted external snapshot = %v, want source %q under template %q", es, legacyTestLocalName, legacyTestTemplateUID)
	}
}
