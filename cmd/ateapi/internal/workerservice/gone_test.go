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

package workerservice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth/ateletauthtest"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// neverCreatedUID is a UID no stored Actor carries.
const neverCreatedUID = "0b6d1f7e-2c3a-4e5d-8f9a-1b2c3d4e5f60"

// Only an Actor with no row is gone: a running one and a suspended one, which
// has no Worker, are not, whatever their state.
func TestListGoneActors(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	seedReportedWorker(t, st, testNode, &ateapipb.WorkerResources{Actors: 1})
	running := seedHostedActor(t, st, testWorkerName)
	suspended := storetest.MustCreateActor(t, ctx, st, &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: suspendAtespace, Name: "suspended"},
		Status:   &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
	})
	deleted := storetest.MustCreateActor(t, ctx, st, &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: suspendAtespace, Name: "deleted"},
		Status:   &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
	})
	if _, err := st.DeleteActor(ctx, resources.ActorRefFromActor(deleted), store.DeletePreconditions{}); err != nil {
		t.Fatalf("deleting actor: %v", err)
	}
	s := New(st, &fakeActors{}, testAteletSPIFFEID, nil)

	got, err := s.ListGoneActors(
		ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)),
		&ateapipb.ListGoneActorsRequest{ActorUids: []string{
			running.GetMetadata().GetUid(),
			deleted.GetMetadata().GetUid(),
			suspended.GetMetadata().GetUid(),
			neverCreatedUID,
		}})
	if err != nil {
		t.Fatalf("ListGoneActors() failed: %v", err)
	}
	want := []string{deleted.GetMetadata().GetUid(), neverCreatedUID}
	if diff := cmp.Diff(want, got.GetGoneActorUids()); diff != "" {
		t.Errorf("gone actor uids (-want +got):\n%s", diff)
	}
}

// failingActorStore fails every lookup of existing Actors.
type failingActorStore struct {
	store.Interface
}

func (failingActorStore) ListExistingActorUIDs(context.Context, []string) ([]string, error) {
	return nil, errors.New("connection reset")
}

// A store that cannot answer names no Actor gone.
func TestListGoneActors_StoreError(t *testing.T) {
	s := New(failingActorStore{}, &fakeActors{}, testAteletSPIFFEID, nil)

	got, err := s.ListGoneActors(
		ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)),
		&ateapipb.ListGoneActorsRequest{ActorUids: []string{neverCreatedUID}})
	if err == nil {
		t.Fatalf("ListGoneActors() = %v, want an error", got)
	}
	if len(got.GetGoneActorUids()) != 0 {
		t.Errorf("gone actor uids = %v on a store error, want none", got.GetGoneActorUids())
	}
}

func TestListGoneActors_Refusals(t *testing.T) {
	s := New(failingActorStore{}, &fakeActors{}, testAteletSPIFFEID, nil)
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode))

	tests := []struct {
		name string
		ctx  context.Context
		uids []string
		want codes.Code
	}{
		{"unauthenticated", ateletauthtest.ContextWith(nil), []string{neverCreatedUID}, codes.Unauthenticated},
		{"no uids", authed, nil, codes.InvalidArgument},
		{"not a uid", authed, []string{"../etc"}, codes.InvalidArgument},
		{"duplicate uid", authed, []string{neverCreatedUID, neverCreatedUID}, codes.InvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.ListGoneActors(tc.ctx, &ateapipb.ListGoneActorsRequest{ActorUids: tc.uids})
			if got := apierror.Code(err); got != tc.want {
				t.Fatalf("code = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}
