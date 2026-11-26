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
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/cmd/ateapi/internal/store/storetest"
	"go.ate.dev/substrate/internal/resources"
)

func crashedActor(message string, snaps ...*ateapi.Snapshot) *ateapi.Actor {
	return &ateapi.Actor{Status: &ateapi.ActorStatus{
		State:     ateapi.ActorState_ACTOR_STATE_CRASHED,
		Crash:     &ateapi.ActorCrash{Message: message},
		Snapshots: snaps,
	}}
}

func TestTransientCrash(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"resume failed: " + crashMessageWorkerDraining, true},
		{crashMessageWorkerPodGone, true},
		{crashMessageWorkerRestarted, true},
		{"resume failed: " + crashMessageWorkerPodGone + ": atelet Restore: runsc start: signal: killed", true},
		{"resume failed: " + crashMessageWorkerIneligible, false},
		{"resume failed: atelet Restore: manifest unknown", false},
		{"killed for exceeding its memory limit", false},
	} {
		if got := transientCrash(crashedActor(tc.message)); got != tc.want {
			t.Errorf("transientCrash(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
	running := crashedActor(crashMessageWorkerDraining)
	running.Status.State = ateapi.ActorState_ACTOR_STATE_RUNNING
	if transientCrash(running) {
		t.Error("transientCrash(RUNNING) = true, want false")
	}
}

func TestRecoverableCrash(t *testing.T) {
	durable := func(gen int32, state ateapi.SnapshotStorageState, locality string) *ateapi.Snapshot {
		return &ateapi.Snapshot{Generation: gen, Locality: locality, DurableSnapshot: &ateapi.SnapshotStorage{State: state}}
	}
	owned := func(snap *ateapi.Snapshot, owner ateapi.SnapshotOwner) *ateapi.Snapshot {
		snap.Owner = owner
		return snap
	}
	completed := ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED
	pending := ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
	for _, tc := range []struct {
		name  string
		actor *ateapi.Actor
		want  bool
	}{
		{"no snapshot", crashedActor("resume failed: " + crashMessageWorkerDraining), true},
		{"latest snapshot uploaded", crashedActor("resume failed: "+crashMessageWorkerDraining, durable(1, completed, ""), durable(2, completed, "node-a")), true},
		{"latest snapshot only on its node", crashedActor("resume failed: "+crashMessageWorkerDraining, durable(1, completed, ""), durable(2, pending, "node-a")), false},
		{"other cause", crashedActor("resume failed: atelet Restore: manifest unknown", durable(1, completed, "")), false},
		{"running workload lost onto its own snapshot", crashedActor(crashMessageWorkerPodGone, owned(durable(1, completed, ""), ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR)), true},
		{"suspend cut onto its own earlier snapshot", crashedActor("suspend failed: "+crashMessageWorkerPodGone, owned(durable(1, completed, ""), ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR), durable(2, pending, "")), true},
		{"running workload lost onto its template's snapshot", crashedActor(crashMessageWorkerPodGone, owned(durable(1, completed, ""), ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG)), false},
		{"running workload lost with no snapshot", crashedActor(crashMessageWorkerRestarted), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recoverableCrash(tc.actor); got != tc.want {
				t.Errorf("recoverableCrash() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecoverTransientCrash(t *testing.T) {
	for _, tc := range []struct {
		name      string
		message   string
		wantState ateapi.ActorState
		wantCrash bool
	}{
		{"transient crash returns to its snapshot", "resume failed: " + crashMessageWorkerDraining, ateapi.ActorState_ACTOR_STATE_SUSPENDED, false},
		{"other crash is left alone", "resume failed: atelet Restore: manifest unknown", ateapi.ActorState_ACTOR_STATE_CRASHED, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			actorRef := resources.ActorRef{Atespace: "team-a", Name: "id1"}
			seedWorkflowActor(t, ctx, st, actorRef, "ns", "tmpl1", ateapi.ActorState_ACTOR_STATE_CRASHED, func(a *ateapi.Actor) {
				a.Status.Crash = &ateapi.ActorCrash{Message: tc.message}
			})

			crash, err := w.recoverTransientCrash(ctx, actorRef)
			if err != nil {
				t.Fatalf("recoverTransientCrash: %v", err)
			}
			if (crash != "") == tc.wantCrash {
				t.Errorf("crash = %q", crash)
			}
			got, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatal(err)
			}
			if got.GetStatus().GetState() != tc.wantState {
				t.Errorf("state = %v, want %v", got.GetStatus().GetState(), tc.wantState)
			}
			if (got.GetStatus().GetCrash() != nil) != tc.wantCrash {
				t.Errorf("crash record = %v, want present: %v", got.GetStatus().GetCrash(), tc.wantCrash)
			}
		})
	}
}

type fakeWorkerGetter struct {
	worker *ateapi.Worker
	err    error
}

func (f fakeWorkerGetter) GetWorker(context.Context, string) (*ateapi.Worker, error) {
	return f.worker, f.err
}

func TestWorkerLeavingCause(t *testing.T) {
	assignment := &ateapi.WorkerAssignment{Worker: &ateapi.ObjectRef{Name: "w1"}, WorkerPodUid: "pod-1"}
	worker := func(state ateapi.WorkerState, podUID string) *ateapi.Worker {
		return &ateapi.Worker{WorkerPodUid: podUID, Status: &ateapi.WorkerStatus{State: state}}
	}
	for _, tc := range []struct {
		name   string
		getter fakeWorkerGetter
		want   string
	}{
		{"serving", fakeWorkerGetter{worker: worker(ateapi.WorkerState_WORKER_STATE_ACTIVE, "pod-1")}, ""},
		{"draining", fakeWorkerGetter{worker: worker(ateapi.WorkerState_WORKER_STATE_DRAINING, "pod-1")}, crashMessageWorkerDraining},
		{"pod replaced", fakeWorkerGetter{worker: worker(ateapi.WorkerState_WORKER_STATE_ACTIVE, "pod-2")}, crashMessageWorkerPodGone},
		{"record gone", fakeWorkerGetter{err: store.ErrNotFound}, crashMessageWorkerPodGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := workerLeavingCause(t.Context(), tc.getter, assignment); got != tc.want {
				t.Errorf("workerLeavingCause() = %q, want %q", got, tc.want)
			}
		})
	}
}
