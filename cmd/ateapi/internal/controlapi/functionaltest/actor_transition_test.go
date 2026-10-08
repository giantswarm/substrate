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

package functionaltest

import (
	"context"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/controlapi"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// TestActorTransitionReconciler_FinishesLostSuspend reproduces an ate-api
// replica that dies between committing SUSPENDING and the atelet's answer:
// the actor is left with the transition and its snapshot destination, and
// nobody retries. The reconciler must take the orphaned suspend up again and
// finish it.
func TestActorTransitionReconciler_FinishesLostSuspend(t *testing.T) {
	ns := namespaceForTest("ns-lost-suspend")
	tc := setupTest(t, ns)
	defer tc.cleanup()

	tmpl := createTemplate(t, tc, ns)
	workerName := createWorkerPod(t, tc, ns, "worker-1", "node1", "pool1")
	ref := &ateapipb.ObjectRef{Atespace: testAtespace, Name: "id1"}
	ctx := context.Background()

	if _, err := tc.client.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: ref.GetName()},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: testAtespace, Name: "tmpl1"},
	}}); err != nil {
		t.Fatalf("CreateActor failed: %v", err)
	}
	if _, err := tc.client.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: ref}); err != nil {
		t.Fatalf("ResumeActor failed: %v", err)
	}

	// The replica serving SuspendActor commits SUSPENDING and the snapshot
	// destination under the actor lease, then dies before the atelet answers:
	// its lease lapses and no caller retries.
	actorRef := resources.ActorRefFromObjectRef(ref)
	running, err := tc.persistence.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor failed: %v", err)
	}
	uri, err := resources.NewActorSnapshotURI(tmpl.GetSnapshotConfig().GetStorageLocation(), testAtespace, running.GetMetadata().GetUid(), resources.NewSnapshotName())
	if err != nil {
		t.Fatalf("NewActorSnapshotURI failed: %v", err)
	}
	stuck, err := tc.persistence.UpdateActor(ctx, actorRef, store.PreconditionFrom(running), func(a *ateapipb.Actor) error {
		a.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDING
		a.Status.InProgressSnapshotUri = uri.String()
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateActor failed: %v", err)
	}

	reconcilerCtx, stop := context.WithCancel(ctx)
	defer stop()
	controlapi.NewActorTransitionReconciler(tc.persistence, tc.service, 100*time.Millisecond, 0).Start(reconcilerCtx)

	deadline := time.Now().Add(30 * time.Second)
	for {
		actor, err := tc.persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor failed: %v", err)
		}
		if actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
			if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() != stuck.GetStatus().GetInProgressSnapshotUri() {
				t.Errorf("external snapshot = %q, want the lost suspend's %q", actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(), stuck.GetStatus().GetInProgressSnapshotUri())
			}
			if actor.GetStatus().GetWorkerAssignment() != nil {
				t.Errorf("worker assignment = %v, want released", actor.GetStatus().GetWorkerAssignment())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actor still %v, want the reconciler to finish the suspend", actor.GetStatus().GetState())
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitForWorkerAvailable(t, tc, workerName)
}
