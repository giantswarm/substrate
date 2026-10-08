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
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestActorTransitionReconciler_FinishesLostSuspend reproduces an ate-api
// replica that dies between committing SUSPENDING and the atelet's answer: the
// suspend's atelet call is lost and nobody retries it. The reconciler must
// take the orphaned suspend up again and finish it once the atelet answers.
func TestActorTransitionReconciler_FinishesLostSuspend(t *testing.T) {
	ns := namespaceForTest("ns-lost-suspend")
	tc := setupTest(t, ns)
	defer tc.cleanup()

	createTemplate(t, tc, ns)
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

	// The atelet's answer never reaches ate-api.
	tc.fakeAtelet.Lock.Lock()
	tc.fakeAtelet.FailCheckpoint = status.Error(codes.Unavailable, "ate-api lost the atelet")
	tc.fakeAtelet.Lock.Unlock()
	if _, err := tc.client.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref}); err == nil {
		t.Fatal("SuspendActor succeeded, want the lost atelet call to fail it")
	}
	actorRef := resources.ActorRefFromObjectRef(ref)
	stuck, err := tc.persistence.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor failed: %v", err)
	}
	if got := stuck.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
		t.Fatalf("state after the lost suspend = %v, want SUSPENDING", got)
	}

	// The atelet is reachable again, and no caller retries.
	tc.fakeAtelet.Lock.Lock()
	tc.fakeAtelet.FailCheckpoint = nil
	tc.fakeAtelet.Lock.Unlock()

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
