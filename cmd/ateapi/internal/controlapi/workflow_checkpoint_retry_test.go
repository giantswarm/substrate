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
	"slices"
	"strings"
	"testing"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/objectstore/objectstoretest"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ateomCheckpointGone is the error atelet returns for a checkpoint whose
// worker's ateom went away with its pod.
var ateomCheckpointGone = status.Error(codes.Unknown, `while calling ateom.CheckpointWorkload: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial unix /var/lib/ate/ateoms/w-1/ateom.sock: connect: no such file or directory"`)

// checkpointDiskFull is a checkpoint failing on a worker that keeps serving.
var checkpointDiskFull = status.Error(codes.Unknown, "while calling ateom.CheckpointWorkload: rpc error: code = Internal desc = writing memory file: no space left on device")

// checkpointCutCrash is the crash a suspend records when the worker drain
// removes the worker under its checkpoint and the crash's teardown.
var checkpointCutCrash = "suspend failed: " + ateletCrashMessage("Checkpoint", ateomCheckpointGone) + " (" + crashMessageWorkerUnreachable + ")"

func TestTransientCheckpointCrash(t *testing.T) {
	crashed := ateapipb.ActorState_ACTOR_STATE_CRASHED
	tests := []struct {
		name     string
		state    ateapipb.ActorState
		crash    string
		snapshot bool
		want     bool
	}{
		{"suspend cut by the drain", crashed, checkpointCutCrash, true, true},
		{"suspend whose worker pod went away", crashed, "suspend failed: " + crashMessageWorkerPodGone, true, true},
		{"suspend on a draining worker", crashed, "suspend failed: " + ateletCrashMessage("Checkpoint", checkpointDiskFull) + " (" + crashMessageWorkerDraining + ")", true, true},
		{"pause cut by the drain", crashed, "pause failed: " + ateletCrashMessage("Checkpoint", ateomCheckpointGone), true, true},
		{"no snapshot to return to", crashed, checkpointCutCrash, false, false},
		{"checkpoint failing on a live worker", crashed, "suspend failed: " + ateletCrashMessage("Checkpoint", checkpointDiskFull), true, false},
		// The pause snapshot is on its node, not with a worker.
		{"paused snapshot upload", crashed, "suspend failed: " + ateletCrashMessage("UploadPausedCheckpoint", ateomCheckpointGone), true, false},
		{"resume crash", crashed, "resume failed: " + crashMessageWorkerDraining, true, false},
		{"crash without an operation", crashed, crashMessageWorkerPodGone, true, false},
		{"recovered since", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, checkpointCutCrash, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: tt.state, Crash: &ateapipb.ActorCrash{Message: tt.crash}}}
			if tt.snapshot {
				actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "gs://bucket/team-a/a/snap-1"}
			}
			if got := transientCheckpointCrash(actor); got != tt.want {
				t.Errorf("transientCheckpointCrash(%v, %q, snapshot %v) = %v, want %v", tt.state, tt.crash, tt.snapshot, got, tt.want)
			}
			if got := transientCrash(actor); got != tt.want && !transientResumeCrash(actor) {
				t.Errorf("transientCrash = %v, want %v", got, tt.want)
			}
		})
	}
}

// checkpointFixture is a RUNNING actor team-a/id1, resumed from its durable
// snapshot snap-1 onto a worker of the resume retry fixture, whose atelet is
// atelet. It returns the snapshot's objects as they are stored.
func checkpointFixture(t *testing.T, atelet *flakyAtelet) (*ActorWorkflow, store.Interface, resources.ActorRef, *ateapipb.ExternalSnapshot, []string) {
	t.Helper()
	ctx := context.Background()
	w, st, actorRef := newResumeRetryFixture(t, atelet)
	objects := w.objectStore.(*objectstoretest.Fake)
	actor, _, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if got := actor.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Fatalf("state after resume = %v, want RUNNING", got)
	}
	snapshot := actor.GetStatus().GetExternalSnapshot()
	if err := objects.WriteSnapshot(snapshot.GetSnapshotUri(), "manifest.json", "memory.img"); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	// The checkpoint writes part of its snapshot before its worker goes away.
	atelet.onCheckpoint = func(req *ateletpb.CheckpointRequest) {
		if err := objects.WriteSnapshot(req.GetExternalConfig().GetSnapshotUri(), "memory.img"); err != nil {
			t.Errorf("WriteSnapshot: %v", err)
		}
	}
	return w, st, actorRef, snapshot, objects.Objects()
}

// assertBackAtSnapshot checks that the actor is SUSPENDED on snapshot with no
// crash, no worker, and no snapshot in progress, and that the store holds
// exactly the objects it held before the checkpoint.
func assertBackAtSnapshot(t *testing.T, w *ActorWorkflow, st store.Interface, actorRef resources.ActorRef, snapshot *ateapipb.ExternalSnapshot, objects []string) {
	t.Helper()
	ctx := context.Background()
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	s := stored.GetStatus()
	if s.GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || s.GetCrash() != nil || s.GetWorkerAssignment() != nil || s.GetInProgressSnapshotUri() != "" {
		t.Errorf("status = %v, want SUSPENDED with no crash, worker or snapshot in progress", s)
	}
	if got, want := s.GetExternalSnapshot().GetSnapshotUri(), snapshot.GetSnapshotUri(); got != want {
		t.Errorf("external snapshot = %q, want the last durable one %q", got, want)
	}
	if got := w.objectStore.(*objectstoretest.Fake).Objects(); !slices.Equal(got, objects) {
		t.Errorf("objects = %v, want %v: the half-written snapshot discarded, the durable one kept", got, objects)
	}
	if _, err := st.FindWorkerHostingActor(ctx, stored.GetMetadata().GetUid()); err == nil {
		t.Error("a worker still hosts the actor, want it freed")
	}
}

// TestSuspendActor_CheckpointCutByDrain suspends a running actor whose worker
// a drain removes under the checkpoint: ateom's socket is gone, and so is the
// worker for the crash's teardown. The suspend answers Unavailable naming the
// cause, the actor is back at its last durable snapshot, the half-written one
// discarded, and a retried suspend and a resume both succeed.
func TestSuspendActor_CheckpointCutByDrain(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{checkpointErrs: []error{ateomCheckpointGone}, terminateErrs: []error{ateomGone}}
	w, st, actorRef, snapshot, objects := checkpointFixture(t, atelet)

	_, err := w.SuspendActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("SuspendActor error = %v, want Unavailable", err)
	}
	if msg := status.Convert(err).Message(); !strings.Contains(msg, "ateom.sock") || !strings.Contains(msg, crashMessageWorkerUnreachable) {
		t.Errorf("SuspendActor error message = %q, want the crash's cause named", msg)
	}
	assertBackAtSnapshot(t, w, st, actorRef, snapshot, objects)

	if actor, err := w.SuspendActor(ctx, actorRef, nil); err != nil || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("retried SuspendActor = (%v, %v), want SUSPENDED", actor.GetStatus().GetState(), err)
	}
	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil || !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("ResumeActor = (%v, %v, %v), want RUNNING and resumed", actor.GetStatus().GetState(), resumed, err)
	}
	if got := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != snapshot.GetSnapshotUri() {
		t.Errorf("resumed from %q, want the last durable snapshot %q", got, snapshot.GetSnapshotUri())
	}
}

// TestSuspendActor_CheckpointCutUntilTheCallEnds keeps the worker unreachable
// for the crash's teardown and the suspend's own recovery: the suspend still
// answers Unavailable, and the actor, CRASHED meanwhile, is returned to its
// snapshot and resumed by the next ResumeActor once the worker is torn down.
func TestSuspendActor_CheckpointCutUntilTheCallEnds(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{checkpointErrs: []error{ateomCheckpointGone}, terminateErrs: []error{ateomGone, ateomGone}}
	w, st, actorRef, snapshot, _ := checkpointFixture(t, atelet)

	_, err := w.SuspendActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("SuspendActor error = %v, want Unavailable", err)
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if !transientCheckpointCrash(stored) {
		t.Fatalf("status = %v, want CRASHED for a transient checkpoint cause", stored.GetStatus())
	}

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil || !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING || actor.GetStatus().GetCrash() != nil {
		t.Fatalf("ResumeActor = (%v, %v, %v), want RUNNING, resumed and no crash", actor.GetStatus(), resumed, err)
	}
	if got := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != snapshot.GetSnapshotUri() {
		t.Errorf("resumed from %q, want the last durable snapshot %q", got, snapshot.GetSnapshotUri())
	}
	assertOneWorkerHosts(t, ctx, st, actor)
}

// TestPauseActor_CheckpointCutByDrain pauses a running actor whose worker a
// drain removes under the checkpoint: the pause answers Unavailable and the
// actor is SUSPENDED on its last durable snapshot.
func TestPauseActor_CheckpointCutByDrain(t *testing.T) {
	atelet := &flakyAtelet{checkpointErrs: []error{ateomCheckpointGone}, terminateErrs: []error{ateomGone}}
	w, st, actorRef, snapshot, objects := checkpointFixture(t, atelet)
	atelet.onCheckpoint = nil

	_, err := w.PauseActor(context.Background(), actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("PauseActor error = %v, want Unavailable", err)
	}
	assertBackAtSnapshot(t, w, st, actorRef, snapshot, objects)
}

// TestSuspendActor_CheckpointCrashStaysCrashed keeps the crashes no snapshot
// undoes: a checkpoint that fails on a worker that keeps serving, and one cut
// by the drain on an actor with no durable snapshot to return to. Both answer
// the crash as before and leave the actor CRASHED.
func TestSuspendActor_CheckpointCrashStaysCrashed(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		noSnapshot bool
	}{
		{name: "checkpoint failing on a live worker", err: checkpointDiskFull},
		{name: "no durable snapshot", err: ateomCheckpointGone, noSnapshot: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			atelet := &flakyAtelet{checkpointErrs: []error{tt.err}}
			w, st, actorRef, _, _ := checkpointFixture(t, atelet)
			if tt.noSnapshot {
				actor, err := st.GetActor(ctx, actorRef)
				if err != nil {
					t.Fatalf("GetActor: %v", err)
				}
				mustUpdateActorStatus(t, ctx, st, actor, func(s *ateapipb.ActorStatus) { s.ExternalSnapshot = nil })
			}

			_, err := w.SuspendActor(ctx, actorRef, nil)
			if err == nil || apierror.Code(err) == codes.Unavailable {
				t.Fatalf("SuspendActor error = %v, want the crash, not Unavailable", err)
			}
			stored, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor: %v", err)
			}
			if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
				t.Errorf("state = %v, want CRASHED", got)
			}
			if _, recovered, err := w.recoverTransientCrash(ctx, actorRef); err != nil || recovered {
				t.Errorf("recoverTransientCrash = (%v, %v), want the actor left alone", recovered, err)
			}
		})
	}
}
