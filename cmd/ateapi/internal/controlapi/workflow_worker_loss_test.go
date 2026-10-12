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
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransientWorkerLossCrash(t *testing.T) {
	crashed := ateapipb.ActorState_ACTOR_STATE_CRASHED
	tests := []struct {
		name     string
		state    ateapipb.ActorState
		crash    string
		snapshot bool
		want     bool
	}{
		{"worker pod gone", crashed, crashMessageWorkerPodGone, true, true},
		{"ateom restarted", crashed, crashMessageAteomRestarted, true, true},
		{"no snapshot to return to", crashed, crashMessageWorkerPodGone, false, false},
		// An operation's crash is its own predicate's.
		{"resume crash", crashed, "resume failed: " + crashMessageWorkerPodGone, true, false},
		{"suspend crash", crashed, "suspend failed: " + crashMessageWorkerPodGone, true, false},
		{"revert crash", crashed, "revert failed: " + crashMessageWorkerPodGone, true, false},
		{"recovered since", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, crashMessageWorkerPodGone, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: tt.state, Crash: &ateapipb.ActorCrash{Message: tt.crash}}}
			if tt.snapshot {
				actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "gs://bucket/team-a/a/snap-1"}
			}
			if got := transientWorkerLossCrash(actor); got != tt.want {
				t.Errorf("transientWorkerLossCrash(%v, %q, snapshot %v) = %v, want %v", tt.state, tt.crash, tt.snapshot, got, tt.want)
			}
			if tt.want && !transientCrash(actor) {
				t.Error("transientCrash = false, want true")
			}
		})
	}
}

// loseWorker deletes the worker hosting the running actor, as when its pod
// goes away with no operation in flight, and returns the actor as stored.
func loseWorker(t *testing.T, st store.Interface, actorRef resources.ActorRef) *ateapipb.Actor {
	t.Helper()
	ctx := context.Background()
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	workerName := actor.GetStatus().GetWorkerAssignment().GetWorker().GetName()
	if _, err := NewWorkerWorkflow(st).DeleteWorker(ctx, workerName, store.DeletePreconditions{}); err != nil {
		t.Fatalf("DeleteWorker(%s): %v", workerName, err)
	}
	if actor, err = st.GetActor(ctx, actorRef); err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if s := actor.GetStatus(); s.GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED || s.GetCrash().GetMessage() != crashMessageWorkerPodGone {
		t.Fatalf("status after the worker went away = %v, want CRASHED for %q", s, crashMessageWorkerPodGone)
	}
	return actor
}

// TestRecoverTransientCrash_RunningActorLostWorker loses the worker under a
// running actor that holds a durable snapshot, with no operation in flight:
// the recovery returns the actor to that snapshot, SUSPENDED, the worker freed
// and the store's objects as they were.
func TestRecoverTransientCrash_RunningActorLostWorker(t *testing.T) {
	w, st, actorRef, snapshot, objects := checkpointFixture(t, &flakyAtelet{})
	crash := loseWorker(t, st, actorRef).GetStatus().GetCrash().GetMessage()

	got, recovered, err := w.recoverTransientCrash(context.Background(), actorRef)
	if err != nil || !recovered || got != crash {
		t.Fatalf("recoverTransientCrash = (%q, %v, %v), want (%q, true, nil)", got, recovered, err, crash)
	}
	assertBackAtSnapshot(t, w, st, actorRef, snapshot, objects)
}

// TestResumeActor_RunningActorLostWorkerResumes resumes a running actor whose
// worker went away under it: ResumeActor returns it to its durable snapshot
// and places it on another worker, RUNNING from that snapshot.
func TestResumeActor_RunningActorLostWorkerResumes(t *testing.T) {
	ctx := context.Background()
	w, st, actorRef, snapshot, _ := checkpointFixture(t, &flakyAtelet{})
	lost := loseWorker(t, st, actorRef)
	lostWorker := lost.GetStatus().GetCrash()

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil || !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING || actor.GetStatus().GetCrash() != nil {
		t.Fatalf("ResumeActor = (%v, %v, %v), want RUNNING, resumed and no crash (was %v)", actor.GetStatus(), resumed, err, lostWorker)
	}
	if got := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != snapshot.GetSnapshotUri() {
		t.Errorf("resumed from %q, want the last durable snapshot %q", got, snapshot.GetSnapshotUri())
	}
	assertOneWorkerHosts(t, ctx, st, actor)
}

// TestResumeActor_RunningActorLostWorkerWithoutSnapshotStaysCrashed loses the
// worker under a running actor with no durable snapshot: nothing survives to
// return to, so the actor stays CRASHED with the cause recorded, and a resume
// is refused rather than retried.
func TestResumeActor_RunningActorLostWorkerWithoutSnapshotStaysCrashed(t *testing.T) {
	ctx := context.Background()
	w, st, actorRef, _, _ := checkpointFixture(t, &flakyAtelet{})
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	mustUpdateActorStatus(t, ctx, st, actor, func(s *ateapipb.ActorStatus) { s.ExternalSnapshot = nil })
	loseWorker(t, st, actorRef)

	if crash, recovered, err := w.recoverTransientCrash(ctx, actorRef); crash != "" || recovered || err != nil {
		t.Errorf("recoverTransientCrash = (%q, %v, %v), want nothing recovered", crash, recovered, err)
	}
	if _, _, err := w.ResumeActor(ctx, actorRef, nil); status.Code(err) == codes.OK || status.Code(err) == codes.Unavailable {
		t.Errorf("ResumeActor error = %v, want the crash's refusal, not a retryable answer", err)
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if s := stored.GetStatus(); s.GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED || s.GetCrash().GetMessage() != crashMessageWorkerPodGone {
		t.Errorf("status = %v, want CRASHED with %q recorded", s, crashMessageWorkerPodGone)
	}
}
