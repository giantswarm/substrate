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

package controlapi

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func fencingToken(holder string, generation int64) *ateapipb.FencingToken {
	return &ateapipb.FencingToken{Holder: holder, Generation: generation}
}

func TestFencingTokenOrder(t *testing.T) {
	tests := []struct {
		name            string
		token, recorded *ateapipb.FencingToken
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
func fencedActor(t *testing.T, ctx context.Context, st store.Interface, state ateapipb.ActorState, recorded *ateapipb.FencingToken) resources.ActorRef {
	t.Helper()
	ref := resources.ActorRef{Atespace: "team-a", Name: "id1"}
	seedWorkflowActor(t, ctx, st, ref, "ns", "tmpl1", state, func(a *ateapipb.Actor) {
		a.Status.FencingToken = recorded
	})
	return ref
}

func mustRecordedFencingToken(t *testing.T, ctx context.Context, st store.Interface, ref resources.ActorRef) (*ateapipb.FencingToken, ateapipb.ActorState) {
	t.Helper()
	actor, err := st.GetActor(ctx, ref)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	return actor.GetStatus().GetFencingToken(), actor.GetStatus().GetState()
}

// TestFencedRequests_RejectStaleToken covers the late request of a
// superseded lease holder: Pause, Suspend and Resume with a token older than
// the recorded one are rejected with FailedPrecondition and leave the actor
// as it was.
func TestFencedRequests_RejectStaleToken(t *testing.T) {
	recorded := fencingToken("executor-b", 2)
	tests := []struct {
		name  string
		state ateapipb.ActorState
		call  func(context.Context, *ActorWorkflow, resources.ActorRef, *ateapipb.FencingToken) error
	}{
		{name: "pause of a running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef, tok *ateapipb.FencingToken) error {
			_, err := w.PauseActor(ctx, ref, tok)
			return err
		}},
		{name: "suspend of a running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef, tok *ateapipb.FencingToken) error {
			_, err := w.SuspendActor(ctx, ref, tok)
			return err
		}},
		{name: "resume of a running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef, tok *ateapipb.FencingToken) error {
			_, _, err := w.ResumeActor(ctx, ref, tok)
			return err
		}},
		{name: "resume of a paused actor", state: ateapipb.ActorState_ACTOR_STATE_PAUSED, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef, tok *ateapipb.FencingToken) error {
			_, _, err := w.ResumeActor(ctx, ref, tok)
			return err
		}},
		{name: "pause of a paused actor", state: ateapipb.ActorState_ACTOR_STATE_PAUSED, call: func(ctx context.Context, w *ActorWorkflow, ref resources.ActorRef, tok *ateapipb.FencingToken) error {
			_, err := w.PauseActor(ctx, ref, tok)
			return err
		}},
	}
	for _, tt := range tests {
		for _, stale := range []*ateapipb.FencingToken{fencingToken("executor-a", 1), fencingToken("executor-a", 2)} {
			t.Run(tt.name, func(t *testing.T) {
				ctx := context.Background()
				st, cleanup := storetest.SetupTestStore(t)
				defer cleanup()
				w := newTestActorWorkflow(t, st, "ns", "tmpl1")
				storetest.MustCreateAtespace(t, ctx, st, "team-a")
				ref := fencedActor(t, ctx, st, tt.state, recorded)

				err := tt.call(ctx, w, ref, stale)
				if got := status.Code(err); got != codes.FailedPrecondition {
					t.Fatalf("status.Code(err) = %v, want %v (err: %v)", got, codes.FailedPrecondition, err)
				}
				gotToken, gotState := mustRecordedFencingToken(t, ctx, st, ref)
				if gotState != tt.state {
					t.Errorf("state = %v, want unchanged %v", gotState, tt.state)
				}
				if !proto.Equal(gotToken, recorded) {
					t.Errorf("recorded token = %v, want unchanged %v", gotToken, recorded)
				}
			})
		}
	}
}

// TestFencedResume_TakeoverFencesOutLatePause is the takeover sequence: the
// new holder resumes the running actor with its newer token, a no-op on the
// runtime that records the token, and the late Pause of the previous holder
// is then rejected; the actor stays RUNNING.
func TestFencedResume_TakeoverFencesOutLatePause(t *testing.T) {
	ctx := context.Background()
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	w := newTestActorWorkflow(t, st, "ns", "tmpl1")
	storetest.MustCreateAtespace(t, ctx, st, "team-a")
	previous, taker := fencingToken("executor-a", 1), fencingToken("executor-b", 2)
	ref := fencedActor(t, ctx, st, ateapipb.ActorState_ACTOR_STATE_RUNNING, previous)

	actor, resumed, err := w.ResumeActor(ctx, ref, taker)
	if err != nil {
		t.Fatalf("ResumeActor with the taker's token: %v", err)
	}
	if resumed {
		t.Error("ResumeActor resumed = true, want false for a running actor")
	}
	if !proto.Equal(actor.GetStatus().GetFencingToken(), taker) {
		t.Errorf("returned token = %v, want %v", actor.GetStatus().GetFencingToken(), taker)
	}

	_, err = w.PauseActor(ctx, ref, previous)
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("late PauseActor: status.Code(err) = %v, want %v (err: %v)", got, codes.FailedPrecondition, err)
	}
	gotToken, gotState := mustRecordedFencingToken(t, ctx, st, ref)
	if gotState != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("state = %v, want RUNNING", gotState)
	}
	if !proto.Equal(gotToken, taker) {
		t.Errorf("recorded token = %v, want %v", gotToken, taker)
	}
}

// TestFencedRequests_AdmitCurrentAndAbsentTokens covers the admitted cases
// on an already-paused actor, where Pause is a no-op: a newer token is
// recorded, the recorded token and an absent one change nothing.
func TestFencedRequests_AdmitCurrentAndAbsentTokens(t *testing.T) {
	recorded := fencingToken("executor-a", 1)
	tests := []struct {
		name  string
		token *ateapipb.FencingToken
		want  *ateapipb.FencingToken
	}{
		{name: "newer token is recorded", token: fencingToken("executor-b", 2), want: fencingToken("executor-b", 2)},
		{name: "recorded token is admitted", token: recorded, want: recorded},
		{name: "absent token is admitted", token: nil, want: recorded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			storetest.MustCreateAtespace(t, ctx, st, "team-a")
			ref := fencedActor(t, ctx, st, ateapipb.ActorState_ACTOR_STATE_PAUSED, recorded)

			if _, err := w.PauseActor(ctx, ref, tt.token); err != nil {
				t.Fatalf("PauseActor: %v", err)
			}
			gotToken, gotState := mustRecordedFencingToken(t, ctx, st, ref)
			if gotState != ateapipb.ActorState_ACTOR_STATE_PAUSED {
				t.Errorf("state = %v, want PAUSED", gotState)
			}
			if !proto.Equal(gotToken, tt.want) {
				t.Errorf("recorded token = %v, want %v", gotToken, tt.want)
			}
		})
	}
}

// TestResumeActor_RecordedTokenTakesRunningFastPath keeps the routed resume
// cheap: a running actor and the token it already recorded need no lease.
func TestResumeActor_RecordedTokenTakesRunningFastPath(t *testing.T) {
	ctx := context.Background()
	persistence := newTestPersistence(t)
	recorded := fencingToken("executor-a", 1)
	storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "id1"},
		Status:   &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_RUNNING, FencingToken: recorded},
	})
	st := &leaseCountingStore{Interface: persistence}
	w := &ActorWorkflow{store: st}

	if _, _, err := w.ResumeActor(ctx, resources.ActorRef{Atespace: "team-a", Name: "id1"}, proto.CloneOf(recorded)); err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if st.acquireCalls != 0 {
		t.Errorf("AcquireLease calls = %d, want 0", st.acquireCalls)
	}
}

// TestFencedRPCs_ValidateToken rejects a token without holder or with a
// generation below one before any workflow runs.
func TestFencedRPCs_ValidateToken(t *testing.T) {
	ctx := context.Background()
	actor := &ateapipb.ObjectRef{Atespace: "team-a", Name: "id1"}
	for _, tok := range []*ateapipb.FencingToken{fencingToken("", 1), fencingToken("executor-a", 0)} {
		if errs := validatePauseActorRequest(ctx, &ateapipb.PauseActorRequest{Actor: actor, FencingToken: tok}); len(errs) == 0 {
			t.Errorf("PauseActorRequest with token %v: want a validation error", tok)
		}
		if errs := validateSuspendActorRequest(ctx, &ateapipb.SuspendActorRequest{Actor: actor, FencingToken: tok}); len(errs) == 0 {
			t.Errorf("SuspendActorRequest with token %v: want a validation error", tok)
		}
		if errs := validateResumeActorRequest(ctx, &ateapipb.ResumeActorRequest{Actor: actor, FencingToken: tok}); len(errs) == 0 {
			t.Errorf("ResumeActorRequest with token %v: want a validation error", tok)
		}
	}
	if errs := validatePauseActorRequest(ctx, &ateapipb.PauseActorRequest{Actor: actor, FencingToken: fencingToken("executor-a", 1)}); len(errs) != 0 {
		t.Errorf("PauseActorRequest with a valid token: %v", errs)
	}
}
