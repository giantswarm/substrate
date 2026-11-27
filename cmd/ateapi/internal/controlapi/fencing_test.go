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

package controlapi

import (
	"context"
	"testing"

	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/apivalidation"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

func fencingToken(holder string, generation int64) *ateapi.FencingToken {
	return &ateapi.FencingToken{Holder: holder, Generation: generation}
}

func TestFencingTokenOrder(t *testing.T) {
	tests := []struct {
		name            string
		token, recorded *ateapi.FencingToken
		stale, newer    bool
	}{
		{name: "absent token is admitted and not recorded", token: nil, recorded: fencingToken("a", 2)},
		{name: "absent token on an unfenced actor", token: nil, recorded: nil},
		{name: "first token is recorded", token: fencingToken("a", 1), recorded: nil, newer: true},
		{name: "higher generation is recorded", token: fencingToken("b", 2), recorded: fencingToken("a", 1), newer: true},
		{name: "same token is admitted without a write", token: fencingToken("a", 2), recorded: fencingToken("a", 2)},
		{name: "lower generation is stale", token: fencingToken("a", 1), recorded: fencingToken("b", 2), stale: true},
		{name: "lower generation of the same holder is stale", token: fencingToken("a", 1), recorded: fencingToken("a", 2), stale: true},
		{name: "same generation under another holder is stale", token: fencingToken("b", 2), recorded: fencingToken("a", 2), stale: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fencingTokenStale(tt.token, tt.recorded); got != tt.stale {
				t.Errorf("fencingTokenStale = %v, want %v", got, tt.stale)
			}
			if got := fencingTokenNewer(tt.token, tt.recorded); got != tt.newer {
				t.Errorf("fencingTokenNewer = %v, want %v", got, tt.newer)
			}
		})
	}
}

// fencedActor seeds an actor in state whose recorded fencing token is
// recorded (none if nil).
func fencedActor(t *testing.T, ctx context.Context, st store.Interface, state ateapi.ActorState, recorded *ateapi.FencingToken) resources.ActorRef {
	t.Helper()
	ref := resources.ActorRef{Atespace: "team-a", Name: "id1"}
	seedWorkflowActor(t, ctx, st, ref, "ns", "tmpl1", state, func(a *ateapi.Actor) {
		a.Status.FencingToken = recorded
	})
	return ref
}

func recordedFencingToken(t *testing.T, ctx context.Context, st store.Interface, ref resources.ActorRef) (*ateapi.FencingToken, ateapi.ActorState) {
	t.Helper()
	actor, err := st.GetActor(ctx, ref)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	return actor.GetStatus().GetFencingToken(), actor.GetStatus().GetState()
}

// A superseded lease holder's Suspend or Resume is rejected with
// FailedPrecondition and changes nothing.
func TestFencedRequests_RejectStaleToken(t *testing.T) {
	recorded := fencingToken("executor-b", 2)
	stale := fencingToken("executor-a", 1)
	for _, tt := range []struct {
		name  string
		state ateapi.ActorState
		call  func(context.Context, *ActorWorkflow, resources.ActorRef) error
	}{
		{name: "suspend of a suspended actor", state: ateapi.ActorState_ACTOR_STATE_SUSPENDED, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef) error {
			_, err := w.SuspendActor(ctx, ref, suspendSync, stale)
			return err
		}},
		{name: "resume of a running actor", state: ateapi.ActorState_ACTOR_STATE_RUNNING, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef) error {
			_, _, err := w.ResumeActor(ctx, ref, stale)
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			st := newTestPersistence(t)
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			ref := fencedActor(t, ctx, st, tt.state, recorded)
			if err := tt.call(ctx, w, ref); apierror.Code(err) != codes.FailedPrecondition {
				t.Fatalf("apierror.Code(err) = %v, want %v (err: %v)", apierror.Code(err), codes.FailedPrecondition, err)
			}
			got, state := recordedFencingToken(t, ctx, st, ref)
			if !proto.Equal(got, recorded) || state != tt.state {
				t.Errorf("recorded token, state = %v, %v; want %v, %v", got, state, recorded, tt.state)
			}
		})
	}
}

// A lease taker's Resume of a running actor records its newer token without
// changing the actor, so the previous holder's late Suspend is fenced out.
func TestFencedResume_TakeoverFencesOutLateSuspend(t *testing.T) {
	ctx := t.Context()
	st := newTestPersistence(t)
	w := newTestActorWorkflow(t, st, "ns", "tmpl1")
	previous := fencingToken("executor-a", 1)
	taker := fencingToken("executor-b", 2)
	ref := fencedActor(t, ctx, st, ateapi.ActorState_ACTOR_STATE_RUNNING, previous)

	actor, resumed, err := w.ResumeActor(ctx, ref, taker)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if resumed || actor.GetStatus().GetState() != ateapi.ActorState_ACTOR_STATE_RUNNING {
		t.Fatalf("ResumeActor = state %v, resumed %v; want RUNNING, not resumed", actor.GetStatus().GetState(), resumed)
	}
	if got, _ := recordedFencingToken(t, ctx, st, ref); !proto.Equal(got, taker) {
		t.Fatalf("recorded token = %v, want %v", got, taker)
	}

	if _, err := w.SuspendActor(ctx, ref, suspendSync, previous); apierror.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late SuspendActor: apierror.Code(err) = %v, want %v (err: %v)", apierror.Code(err), codes.FailedPrecondition, err)
	}
	if _, state := recordedFencingToken(t, ctx, st, ref); state != ateapi.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("state = %v, want RUNNING", state)
	}
}

// The current token and an absent one are admitted; a newer one is recorded.
func TestFencedSuspend_AdmitsCurrentAbsentAndNewerTokens(t *testing.T) {
	recorded := fencingToken("executor-a", 2)
	for _, tt := range []struct {
		name  string
		token *ateapi.FencingToken
		want  *ateapi.FencingToken
	}{
		{name: "absent", token: nil, want: recorded},
		{name: "current", token: fencingToken("executor-a", 2), want: recorded},
		{name: "newer", token: fencingToken("executor-b", 3), want: fencingToken("executor-b", 3)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			st := newTestPersistence(t)
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			ref := fencedActor(t, ctx, st, ateapi.ActorState_ACTOR_STATE_SUSPENDED, recorded)
			if _, err := w.SuspendActor(ctx, ref, suspendSync, tt.token); err != nil {
				t.Fatalf("SuspendActor: %v", err)
			}
			if got, _ := recordedFencingToken(t, ctx, st, ref); !proto.Equal(got, tt.want) {
				t.Errorf("recorded token = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFencedRPCs_ValidateToken(t *testing.T) {
	ctx := t.Context()
	actor := &ateapi.ObjectRef{Atespace: "team-a", Name: "id1"}
	for _, tok := range []*ateapi.FencingToken{{Holder: "", Generation: 1}, {Holder: "executor-a", Generation: 0}} {
		if errs := apivalidation.ValidateSuspendActorRequest(ctx, &ateapi.SuspendActorRequest{Actor: actor, FencingToken: tok}); len(errs) == 0 {
			t.Errorf("SuspendActorRequest with token %v: want a validation error", tok)
		}
		if errs := apivalidation.ValidateResumeActorRequest(ctx, &ateapi.ResumeActorRequest{Actor: actor, FencingToken: tok}); len(errs) == 0 {
			t.Errorf("ResumeActorRequest with token %v: want a validation error", tok)
		}
	}
	valid := fencingToken("executor-a", 1)
	if errs := apivalidation.ValidateSuspendActorRequest(ctx, &ateapi.SuspendActorRequest{Actor: actor, FencingToken: valid}); len(errs) != 0 {
		t.Errorf("SuspendActorRequest with a valid token: %v", errs)
	}
	if errs := apivalidation.ValidateResumeActorRequest(ctx, &ateapi.ResumeActorRequest{Actor: actor, FencingToken: valid}); len(errs) != 0 {
		t.Errorf("ResumeActorRequest with a valid token: %v", errs)
	}
}
