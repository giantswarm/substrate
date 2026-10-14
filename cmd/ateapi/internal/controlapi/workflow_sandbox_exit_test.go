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
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
)

const sandboxExitReason = "sandbox process 42 exited"

// exitWorkflow is crashWorkflow with the deadline a workflow that takes the
// actor's lease needs, and the atelet whose Terminate requests it records.
func exitWorkflow(t *testing.T, st store.Interface) (*ActorWorkflow, *terminatingAtelet) {
	t.Helper()
	atelet := &terminatingAtelet{}
	return &ActorWorkflow{store: st, dialer: newFakeAteletDialer(t, atelet), workflowDeadline: time.Minute}, atelet
}

// A running actor whose sandbox exited right after it was recorded RUNNING is
// crashed with the worker's observation, torn down on its worker and freed,
// so its client sees the crash and can revert it instead of waiting on an
// actor nothing runs behind.
func TestCrashExitedActor(t *testing.T) {
	ctx := t.Context()
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	seedActor(t, ctx, st, actorRef)
	seedWorker(t, ctx, st, actorRef)
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor() = %v", err)
	}
	w, atelet := exitWorkflow(t, st)

	got, err := w.CrashExitedActor(ctx, actorRef, actor.GetMetadata().GetUid(), "uid", sandboxExitReason)
	if err != nil {
		t.Fatalf("CrashExitedActor() = %v, want nil", err)
	}
	assertCrashed(t, ctx, st, actorRef)
	if state := got.GetStatus().GetState(); state != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Errorf("answered state = %v, want CRASHED", state)
	}
	wantMsg := crashMessageSandboxExited + ": " + sandboxExitReason
	if msg := got.GetStatus().GetCrash().GetMessage(); msg != wantMsg {
		t.Errorf("crash message = %q, want %q", msg, wantMsg)
	}
	if n := len(atelet.terminateRequests()); n != 1 {
		t.Errorf("Terminate requests = %d, want 1: the crash tears down what is left on the worker", n)
	}
}

func TestCrashExitedActor_Refused(t *testing.T) {
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	tests := []struct {
		name string
		// state the seeded actor is moved to; RUNNING when zero.
		state  ateapipb.ActorState
		uid    string // empty: the actor's own
		worker string
		want   codes.Code
	}{
		{name: "stale incarnation", uid: "6a0b3f1e-8c2d-4e5f-9a7b-1c2d3e4f5a6b", worker: "uid", want: codes.NotFound},
		{name: "hosted by another worker", worker: "other-worker", want: codes.NotFound},
		// An operation that moved the actor on fails on the exited sandbox
		// itself; the report must not crash it from under that operation.
		{name: "suspending", state: ateapipb.ActorState_ACTOR_STATE_SUSPENDING, worker: "uid", want: codes.FailedPrecondition},
		{name: "resuming", state: ateapipb.ActorState_ACTOR_STATE_RESUMING, worker: "uid", want: codes.FailedPrecondition},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			seedActor(t, ctx, st, actorRef)
			actor, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor() = %v", err)
			}
			if tc.state != ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED {
				actor, err = st.UpdateActor(ctx, actorRef, store.PreconditionFrom(actor), func(a *ateapipb.Actor) error {
					a.Status.State = tc.state
					return nil
				})
				if err != nil {
					t.Fatalf("UpdateActor() = %v", err)
				}
			}
			uid := tc.uid
			if uid == "" {
				uid = actor.GetMetadata().GetUid()
			}
			w, atelet := exitWorkflow(t, st)

			_, err = w.CrashExitedActor(ctx, actorRef, uid, tc.worker, sandboxExitReason)
			if got := apierror.Code(err); got != tc.want {
				t.Fatalf("CrashExitedActor() code = %v (err %v), want %v", got, err, tc.want)
			}
			after, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor() = %v", err)
			}
			if after.GetStatus().GetState() != actor.GetStatus().GetState() {
				t.Errorf("state = %v, want %v left as it was", after.GetStatus().GetState(), actor.GetStatus().GetState())
			}
			if n := len(atelet.terminateRequests()); n != 0 {
				t.Errorf("Terminate requests = %d, want 0", n)
			}
		})
	}
}

// An operation holding the actor's lease is waited out by the worker, which
// retries an Aborted report.
func TestCrashExitedActor_LeaseHeld(t *testing.T) {
	ctx := t.Context()
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	seedActor(t, ctx, st, actorRef)
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor() = %v", err)
	}
	held, err := st.AcquireLease(ctx, actorLeaseKey(actorRef), ateattr.OperationSuspend)
	if err != nil {
		t.Fatalf("AcquireLease() = %v", err)
	}
	defer held.Close()
	w, _ := exitWorkflow(t, st)

	_, err = w.CrashExitedActor(ctx, actorRef, actor.GetMetadata().GetUid(), "uid", sandboxExitReason)
	if got := apierror.Code(err); got != codes.Aborted {
		t.Errorf("CrashExitedActor() code = %v (err %v), want Aborted", got, err)
	}
}
