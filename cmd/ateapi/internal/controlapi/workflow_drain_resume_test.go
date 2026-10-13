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
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
)

// drainWorker moves the named pod's Worker to DRAINING, as the worker syncer
// does once the pod is Terminating.
func drainWorker(t *testing.T, st store.Interface, pod string) {
	t.Helper()
	ctx := context.Background()
	worker, err := st.GetWorker(ctx, testWorkerUID(pod))
	if err != nil {
		t.Errorf("GetWorker(%s): %v", pod, err)
		return
	}
	if _, err := st.UpdateWorker(ctx, worker.GetMetadata().GetName(), store.PreconditionFrom(worker), func(toUpdate *ateapipb.Worker) error {
		toUpdate.Status.State = ateapipb.WorkerState_WORKER_STATE_DRAINING
		return nil
	}); err != nil {
		t.Errorf("UpdateWorker(%s): %v", pod, err)
	}
}

// hostingWorker names the worker the stored actor is bound to.
func hostingWorker(t *testing.T, st store.Interface, actorRef resources.ActorRef) string {
	t.Helper()
	ctx := context.Background()
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Errorf("GetActor: %v", err)
		return ""
	}
	name, err := st.FindWorkerHostingActor(ctx, actor.GetMetadata().GetUid())
	if err != nil {
		t.Errorf("FindWorkerHostingActor: %v", err)
		return ""
	}
	return name
}

// assertSuspendedOnSnapshot checks the actor is back at rest on its snapshot,
// holding no worker and carrying no crash.
func assertSuspendedOnSnapshot(t *testing.T, st store.Interface, actorRef resources.ActorRef, snapshotURI string) {
	t.Helper()
	ctx := context.Background()
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("state = %v, want SUSPENDED", got)
	}
	if stored.GetStatus().GetCrash() != nil || stored.GetStatus().GetWorkerAssignment() != nil {
		t.Errorf("status = %v, want no crash and no worker", stored.GetStatus())
	}
	if got := stored.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != snapshotURI {
		t.Errorf("external snapshot = %q, want it intact at %q", got, snapshotURI)
	}
	if _, err := st.FindWorkerHostingActor(ctx, stored.GetMetadata().GetUid()); err == nil {
		t.Error("a worker still hosts the actor, want it freed")
	}
}

// TestResumeActor_NeverPlacesOnADrainingWorker resumes an actor while a worker
// pool drains. A DRAINING worker with room is never a placement target: the
// actor goes to the ACTIVE worker beside it, and with no ACTIVE worker left
// the resume answers ResourceExhausted, which the caller waits out, and leaves
// the actor at rest on its snapshot without touching atelet.
func TestResumeActor_NeverPlacesOnADrainingWorker(t *testing.T) {
	t.Run("an active worker remains", func(t *testing.T) {
		ctx := context.Background()
		atelet := &flakyAtelet{}
		w, st, actorRef := newResumeRetryFixture(t, atelet)
		drainWorker(t, st, "pod-1")

		actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
		if err != nil {
			t.Fatalf("ResumeActor: %v", err)
		}
		if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			t.Errorf("ResumeActor = (state %v, resumed %v), want RUNNING and resumed", actor.GetStatus().GetState(), resumed)
		}
		if got, want := actor.GetStatus().GetWorkerAssignment().GetWorker().GetName(), testWorkerUID("pod-2"); got != want {
			t.Errorf("actor runs on worker %q, want the active one %q", got, want)
		}
		if restores, _ := atelet.counts(); restores != 1 {
			t.Errorf("Restore calls = %d, want 1", restores)
		}
	})

	t.Run("every worker drains", func(t *testing.T) {
		ctx := context.Background()
		atelet := &flakyAtelet{}
		w, st, actorRef := newResumeRetryFixture(t, atelet)
		before, err := st.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		drainWorker(t, st, "pod-1")
		drainWorker(t, st, "pod-2")

		_, _, err = w.ResumeActor(ctx, actorRef, nil)
		if got := apierror.Code(err); got != codes.ResourceExhausted {
			t.Fatalf("ResumeActor error = %v, want ResourceExhausted", err)
		}
		assertSuspendedOnSnapshot(t, st, actorRef, before.GetStatus().GetExternalSnapshot().GetSnapshotUri())
		if restores, terminates := atelet.counts(); restores != 0 || terminates != 0 {
			t.Errorf("atelet calls = %d Restore, %d Terminate, want none", restores, terminates)
		}
	})
}

// TestResumeActor_WorkerLeavesUnderASuccessfulRestore resumes an actor whose
// worker starts leaving while a restore that succeeds runs on it: it drains,
// or its record is already gone. The resume never answers RUNNING on that
// worker: the actor is crashed by the resume, returned to its snapshot, and
// placed on the other worker.
func TestResumeActor_WorkerLeavesUnderASuccessfulRestore(t *testing.T) {
	tests := []struct {
		name  string
		leave func(t *testing.T, st store.Interface, worker string)
	}{
		{"draining", func(t *testing.T, st store.Interface, worker string) {
			w, err := st.GetWorker(context.Background(), worker)
			if err != nil {
				t.Errorf("GetWorker: %v", err)
				return
			}
			if _, err := st.UpdateWorker(context.Background(), worker, store.PreconditionFrom(w), func(toUpdate *ateapipb.Worker) error {
				toUpdate.Status.State = ateapipb.WorkerState_WORKER_STATE_DRAINING
				return nil
			}); err != nil {
				t.Errorf("UpdateWorker: %v", err)
			}
		}},
		{"gone", func(t *testing.T, st store.Interface, worker string) {
			if _, err := st.DeleteWorker(context.Background(), worker, store.DeletePreconditions{}); err != nil {
				t.Errorf("DeleteWorker: %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var st store.Interface
			var actorRef resources.ActorRef
			var leftWorker string
			atelet := &flakyAtelet{}
			atelet.onRestore = func(n int) {
				if n != 1 {
					return
				}
				leftWorker = hostingWorker(t, st, actorRef)
				tt.leave(t, st, leftWorker)
			}
			w, fixtureStore, ref := newResumeRetryFixture(t, atelet)
			st, actorRef = fixtureStore, ref
			before, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor: %v", err)
			}

			actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
			if err != nil {
				t.Fatalf("ResumeActor: %v", err)
			}
			if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING || actor.GetStatus().GetCrash() != nil {
				t.Errorf("ResumeActor = (status %v, resumed %v), want RUNNING, resumed and no crash", actor.GetStatus(), resumed)
			}
			if got := actor.GetStatus().GetWorkerAssignment().GetWorker().GetName(); got == leftWorker {
				t.Errorf("actor runs on worker %q, which left under the restore; want the other worker", got)
			}
			if got, want := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(), before.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != want {
				t.Errorf("external snapshot = %q, want it intact at %q", got, want)
			}
			if restores, terminates := atelet.counts(); restores != 2 || terminates != 1 {
				t.Errorf("atelet calls = %d Restore, %d Terminate, want 2 and 1: the restore on the leaving worker torn down, then the resume", restores, terminates)
			}
			assertOneWorkerHosts(t, ctx, st, actor)
		})
	}
}

// TestResumeActor_OnlyWorkerDrainsUnderASuccessfulRestore resumes an actor
// whose only worker drains under a restore that succeeds and never comes
// back. The call answers Unavailable when its context ends, never RUNNING on
// the draining worker, and leaves the actor at rest on its snapshot for the
// caller's retry.
func TestResumeActor_OnlyWorkerDrainsUnderASuccessfulRestore(t *testing.T) {
	var st store.Interface
	atelet := &flakyAtelet{}
	atelet.onRestore = func(n int) {
		if n == 1 {
			drainWorker(t, st, "pod-1")
		}
	}
	w, fixtureStore, actorRef := newResumeRetryFixtureOn(t, []string{"pod-1"}, atelet)
	st = fixtureStore
	before, err := st.GetActor(context.Background(), actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	_, _, err = w.ResumeActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ResumeActor error = %v, want Unavailable", err)
	}
	assertSuspendedOnSnapshot(t, st, actorRef, before.GetStatus().GetExternalSnapshot().GetSnapshotUri())
	if restores, terminates := atelet.counts(); restores != 1 || terminates != 1 {
		t.Errorf("atelet calls = %d Restore, %d Terminate, want 1 and 1: the restore on the draining worker torn down, no other worker", restores, terminates)
	}
}
