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
	"google.golang.org/protobuf/proto"
)

// writtenActorStore records the last Actor an UpdateActor wrote, as the
// mutation left it, before any read migration sees the row again.
type writtenActorStore struct {
	store.Interface
	written *ateapipb.Actor
}

func (s *writtenActorStore) UpdateActor(ctx context.Context, actorRef resources.ActorRef, precondition store.Precondition, mutate func(toUpdate *ateapipb.Actor) error) (*ateapipb.Actor, error) {
	return s.Interface.UpdateActor(ctx, actorRef, precondition, func(toUpdate *ateapipb.Actor) error {
		if err := mutate(toUpdate); err != nil {
			return err
		}
		s.written = proto.Clone(toUpdate).(*ateapipb.Actor)
		return nil
	})
}

// TestEnsurePausedFinalized_WritesNoCrashOnPaused checks the row the pause
// commit writes over an actor crashed after its checkpoint. The read
// migration drops a crash record on an actor that is not CRASHED, so a read
// back cannot tell whether the commit wrote one.
func TestEnsurePausedFinalized_WritesNoCrashOnPaused(t *testing.T) {
	ctx := context.Background()
	persistence := newTestPersistence(t)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	workerName := testWorkerUID("worker-pod-1")

	created := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
		Status: &ateapipb.ActorStatus{
			State:        ateapipb.ActorState_ACTOR_STATE_CRASHED,
			Crash:        &ateapipb.ActorCrash{Message: "worker pod is gone"},
			AssignedNode: "node1",
			WorkerAssignment: &ateapipb.WorkerAssignment{
				Worker:          &ateapipb.ObjectRef{Name: workerName},
				WorkerNamespace: "default",
				WorkerPool:      "pool1",
				WorkerPod:       "worker-pod-1",
				WorkerPodUid:    workerName,
			},
			InProgressLocalSnapshotName: "local-snap-1",
		},
	})
	if _, err := persistence.CreateWorker(ctx, &ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: workerName},
		WorkerNamespace: "default",
		WorkerPool:      "pool1",
		WorkerPod:       "worker-pod-1",
		WorkerPodUid:    workerName,
		NodeName:        "node1",
		Status:          &ateapipb.WorkerStatus{},
	}); err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	seedAssignment(t, persistence, workerName, &ateapipb.ActorAssignment{
		Actor:    &ateapipb.ObjectRef{Atespace: actorRef.Atespace, Name: actorRef.Name},
		ActorUid: created.GetMetadata().GetUid(),
	})

	st := &writtenActorStore{Interface: persistence}
	w := &ActorWorkflow{store: st}
	if _, err := w.ensurePausedFinalized(ctx, actorRef, &ateapipb.ActorTemplate{}); err != nil {
		t.Fatalf("ensurePausedFinalized: %v", err)
	}

	if st.written == nil {
		t.Fatal("ensurePausedFinalized wrote no Actor")
	}
	if got := st.written.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		t.Errorf("written state = %v, want PAUSED", got)
	}
	if crash := st.written.GetStatus().GetCrash(); crash != nil {
		t.Errorf("written crash = %v, want none on a PAUSED actor", crash)
	}
}
