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
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth/ateletauthtest"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

const exitReason = "sandbox process 42 exited"

func exitRequest(actorUID string) *ateapipb.ReportActorExitRequest {
	return &ateapipb.ReportActorExitRequest{
		Worker:   &ateapipb.ObjectRef{Name: testWorkerName},
		Actor:    &ateapipb.ObjectRef{Atespace: suspendAtespace, Name: suspendActor},
		ActorUid: actorUID,
		Reason:   exitReason,
	}
}

// A worker's report reaches the crash with the Worker its atelet proved and
// the incarnation and reason it named.
func TestReportActorExit(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	seedReportedWorker(t, st, testNode, &ateapipb.WorkerResources{Actors: 1})
	actor := seedHostedActor(t, st, testWorkerName)
	actors := &fakeActors{}
	s := New(st, actors, testAteletSPIFFEID, nil)

	got, err := s.ReportActorExit(
		ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)),
		exitRequest(actor.GetMetadata().GetUid()))
	if err != nil {
		t.Fatalf("ReportActorExit() failed: %v", err)
	}
	if state := got.GetActor().GetStatus().GetState(); state != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Errorf("answered actor state = %v, want CRASHED", state)
	}
	want := []crashExitedCall{{
		actorRef:   resources.ActorRef{Atespace: suspendAtespace, Name: suspendActor},
		actorUID:   actor.GetMetadata().GetUid(),
		workerName: testWorkerName,
		reason:     exitReason,
	}}
	if diff := cmp.Diff(want, actors.crashes, cmp.AllowUnexported(crashExitedCall{})); diff != "" {
		t.Errorf("crashes mismatch (-want +got):\n%s", diff)
	}
}

func TestReportActorExit_Refused(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	seedReportedWorker(t, st, testNode, &ateapipb.WorkerResources{Actors: 1})
	actor := seedHostedActor(t, st, testWorkerName)
	uid := actor.GetMetadata().GetUid()
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode))
	noReason := exitRequest(uid)
	noReason.Reason = ""
	absentWorker := exitRequest(uid)
	absentWorker.Worker = &ateapipb.ObjectRef{Name: "5c7e2a91-3d4b-4f60-8a1c-2e9b7d0f4a63"}

	tests := []struct {
		name string
		ctx  context.Context
		req  *ateapipb.ReportActorExitRequest
		want codes.Code
	}{
		{"unauthenticated", ateletauthtest.ContextWith(nil), exitRequest(uid), codes.Unauthenticated},
		{"no reason", authed, noReason, codes.InvalidArgument},
		{"no actor uid", authed, exitRequest(""), codes.InvalidArgument},
		// Not disclosed: a Worker on another node reads as absent.
		{"worker on another node", ateletauthtest.ContextWith(ateletauthtest.CertOn(t, "some-other-node")), exitRequest(uid), codes.NotFound},
		{"absent worker", authed, absentWorker, codes.NotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actors := &fakeActors{}
			s := New(st, actors, testAteletSPIFFEID, nil)
			_, err := s.ReportActorExit(tc.ctx, tc.req)
			if got := apierror.Code(err); got != tc.want {
				t.Fatalf("code = %v (err %v), want %v", got, err, tc.want)
			}
			if len(actors.crashes) != 0 {
				t.Errorf("refused report still reached the crash: %v", actors.crashes)
			}
		})
	}
}

// The workflow's refusal is the answer, passed through as given.
func TestReportActorExit_SurfacesRefusal(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	seedReportedWorker(t, st, testNode, &ateapipb.WorkerResources{Actors: 1})
	actor := seedHostedActor(t, st, testWorkerName)
	actors := &fakeActors{crashErr: apierror.FailedPrecondition("Actor is SUSPENDING")}
	s := New(st, actors, testAteletSPIFFEID, nil)

	_, err := s.ReportActorExit(
		ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)),
		exitRequest(actor.GetMetadata().GetUid()))
	if got := apierror.Code(err); got != codes.FailedPrecondition {
		t.Errorf("code = %v (err %v), want FailedPrecondition", got, err)
	}
}
