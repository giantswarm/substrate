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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

func TestTransientWorkerLossCrash(t *testing.T) {
	crashed := ateapipb.ActorState_ACTOR_STATE_CRASHED
	tests := []struct {
		name     string
		state    ateapipb.ActorState
		crash    string
		snapshot bool
		golden   bool
		want     bool
	}{
		{"worker pod gone", crashed, crashMessageWorkerPodGone, true, false, true},
		{"ateom restarted", crashed, crashMessageAteomRestarted, true, false, true},
		{"no durable snapshot to return to", crashed, crashMessageWorkerPodGone, false, false, false},
		// An actor that never suspended holds its template's golden, which
		// carries none of what it did: going back there would reset it.
		{"only the golden to return to", crashed, crashMessageWorkerPodGone, false, true, false},
		// An operation's crash is its own predicate's.
		{"resume crash", crashed, "resume failed: " + crashMessageWorkerPodGone, true, false, false},
		{"suspend crash", crashed, "suspend failed: " + crashMessageWorkerPodGone, true, false, false},
		{"revert crash", crashed, "revert failed: " + crashMessageWorkerPodGone, true, false, false},
		{"recovered since", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, crashMessageWorkerPodGone, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: tt.state, Crash: &ateapipb.ActorCrash{Message: tt.crash}}}
			if tt.snapshot {
				actor.Status.Snapshots = []*ateapipb.Snapshot{durableSnapshot("gs://snapshots/team-a/a/snap-1", ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED)}
			}
			if tt.golden {
				actor.Status.Snapshots = []*ateapipb.Snapshot{newDurableSnapshot(1, ateapipb.SnapshotOwner_SNAPSHOT_OWNER_TAG, ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY, "", "gs://snapshots/atespaces/ate-golden/tags/golden-1", ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED)}
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

// TestRecoverTransientCrash_WorkerLoss returns a running actor whose worker
// went away under it, with no operation in flight, to its last completed
// durable snapshot; one with no durable snapshot stays CRASHED with its cause.
func TestRecoverTransientCrash_WorkerLoss(t *testing.T) {
	for _, tt := range []struct {
		name          string
		noSnapshot    bool
		wantRecovered bool
	}{
		{name: "returns to the durable snapshot", wantRecovered: true},
		{name: "no durable snapshot stays crashed", noSnapshot: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			storetest.MustCreateAtespace(t, ctx, st, "team-a")
			actorRef := resources.ActorRef{Atespace: "team-a", Name: "id1"}
			completed, _ := seedCheckpointCrash(t, ctx, st, actorRef, crashMessageWorkerPodGone, tt.noSnapshot)

			crash, recovered, err := w.recoverTransientCrash(ctx, actorRef)
			if err != nil || recovered != tt.wantRecovered {
				t.Fatalf("recoverTransientCrash = (%q, %v, %v), want recovered %v", crash, recovered, err, tt.wantRecovered)
			}
			if tt.wantRecovered {
				assertBackAtSnapshot(t, ctx, st, actorRef, completed)
				return
			}
			stored, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor: %v", err)
			}
			if s := stored.GetStatus(); s.GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED || s.GetCrash().GetMessage() != crashMessageWorkerPodGone {
				t.Errorf("status = %v, want CRASHED with %q recorded", s, crashMessageWorkerPodGone)
			}
		})
	}
}
