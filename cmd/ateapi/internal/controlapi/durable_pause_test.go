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
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/objectstore/objectstoretest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const pauseSnapshot = "pause-snap-1"

// pausedOn builds the status of an actor paused on a local snapshot on node.
func pausedOn(node string) *ateapipb.ActorStatus {
	return &ateapipb.ActorStatus{
		State: ateapipb.ActorState_ACTOR_STATE_PAUSED,
		LocalSnapshotInfo: &ateapipb.LocalSnapshotInfo{
			SnapshotName:              pauseSnapshot,
			NodeVmsWithLocalSnapshots: []string{node},
			ContentScope:              ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
		},
	}
}

// durableCopyOf is the external snapshot the background upload of the pause
// snapshot records.
func durableCopyOf(uri string) *ateapipb.ExternalSnapshot {
	return &ateapipb.ExternalSnapshot{
		SnapshotUri:             uri,
		ContentScope:            ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
		SourceLocalSnapshotName: pauseSnapshot,
	}
}

// TestDurablePauseCopy pins what counts as the durable copy of a pause: only
// an external snapshot uploaded from the very local snapshot the actor holds.
// Anything else — the snapshot of an earlier suspend, one uploaded from a
// previous pause — is older state and must never stand in for the pause.
func TestDurablePauseCopy(t *testing.T) {
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	tests := []struct {
		name   string
		status *ateapipb.ActorStatus
		want   bool
	}{
		{"no local snapshot", &ateapipb.ActorStatus{ExternalSnapshot: durableCopyOf(uri)}, false},
		{"no external snapshot", pausedOn("node1"), false},
		{"external snapshot from an earlier suspend", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri}
			return st
		}(), false},
		{"external snapshot uploaded from an earlier pause", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = durableCopyOf(uri)
			st.ExternalSnapshot.SourceLocalSnapshotName = "pause-snap-0"
			return st
		}(), false},
		{"the upload of the pause snapshot", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = durableCopyOf(uri)
			return st
		}(), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := durablePauseCopy(&ateapipb.Actor{Status: tc.status})
			if (got != nil) != tc.want {
				t.Errorf("durablePauseCopy = %v, want present %t", got, tc.want)
			}
		})
	}
}

// TestPauseAwaitsResync pins which stored actors the resync uploads: paused
// on a node, without a durable copy, and older than the grace the upload
// queued at pause time gets.
func TestPauseAwaitsResync(t *testing.T) {
	now := time.Now()
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	old := timestamppb.New(now.Add(-2 * pauseUploadResyncGrace))
	fresh := timestamppb.New(now.Add(-pauseUploadResyncGrace / 2))
	tests := []struct {
		name  string
		actor *ateapipb.Actor
		want  bool
	}{
		{"paused without a copy, past the grace", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: pausedOn("node1")}, true},
		{"paused without a copy, within the grace", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: fresh}, Status: pausedOn("node1")}, false},
		{"paused with a copy", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = durableCopyOf(uri)
			return st
		}()}, false},
		{"paused with no node recorded", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: &ateapipb.ActorStatus{
			State:             ateapipb.ActorState_ACTOR_STATE_PAUSED,
			LocalSnapshotInfo: &ateapipb.LocalSnapshotInfo{SnapshotName: pauseSnapshot},
		}}, false},
		{"suspended", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: &ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: uri},
		}}, false},
		{"running with a stale local snapshot record", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
			return st
		}()}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pauseAwaitsResync(tc.actor, now); got != tc.want {
				t.Errorf("pauseAwaitsResync = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestSchedulingConstraints_PauseNodes verifies the placement a paused actor
// gets: confined to its snapshot's node while that node holds the only copy,
// only preferring it once the snapshot has a durable copy.
func TestSchedulingConstraints_PauseNodes(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{SandboxConfig: &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR}}
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)

	t.Run("only copy is local", func(t *testing.T) {
		c, err := schedulingConstraints(&ateapipb.Actor{Status: pausedOn("node1")}, tmpl)
		if err != nil {
			t.Fatalf("schedulingConstraints: %v", err)
		}
		if len(c.RequiredNodes) != 1 || c.RequiredNodes[0] != "node1" {
			t.Errorf("RequiredNodes = %v, want [node1]", c.RequiredNodes)
		}
		if len(c.PreferredNodes) != 0 {
			t.Errorf("PreferredNodes = %v, want none", c.PreferredNodes)
		}
	})
	t.Run("durable copy exists", func(t *testing.T) {
		st := pausedOn("node1")
		st.ExternalSnapshot = durableCopyOf(uri)
		c, err := schedulingConstraints(&ateapipb.Actor{Status: st}, tmpl)
		if err != nil {
			t.Fatalf("schedulingConstraints: %v", err)
		}
		if len(c.RequiredNodes) != 0 {
			t.Errorf("RequiredNodes = %v, want none", c.RequiredNodes)
		}
		if len(c.PreferredNodes) != 1 || c.PreferredNodes[0] != "node1" {
			t.Errorf("PreferredNodes = %v, want [node1]", c.PreferredNodes)
		}
	})
	t.Run("older external snapshot keeps the node required", func(t *testing.T) {
		st := pausedOn("node1")
		st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri}
		c, err := schedulingConstraints(&ateapipb.Actor{Status: st}, tmpl)
		if err != nil {
			t.Fatalf("schedulingConstraints: %v", err)
		}
		if len(c.RequiredNodes) != 1 || len(c.PreferredNodes) != 0 {
			t.Errorf("RequiredNodes = %v, PreferredNodes = %v; want the node required", c.RequiredNodes, c.PreferredNodes)
		}
	})
}

// TestRecordDurablePauseCopy covers how an uploaded pause snapshot lands on
// the record: recorded and the snapshot it replaces released while the actor
// is still paused on that snapshot; discarded when the actor moved on before
// the upload finished; left alone when another replica already recorded it;
// deferred while a lifecycle workflow holds the actor's lease, so a resume
// that read the record never meets a version it did not see.
func TestRecordDurablePauseCopy(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{SnapshotsConfig: &ateapipb.SnapshotsConfig{StorageLocation: testStorageLocation}}
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	seed := func(t *testing.T) (*ActorWorkflow, store.Interface, *objectstoretest.Fake, *ateapipb.Actor, *ateapipb.ExternalSnapshot) {
		t.Helper()
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w, objects := newFinalizeWorkflow(persistence)
		created := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
			Status:   pausedOn("node1"),
		})
		previous := mustActorSnapshotURI(t, tmpl, created, "suspend-snap-0")
		objects.PutSnapshot(t, previous, "manifest.json", "memory.zst")
		actor := mustUpdateActorStatus(t, ctx, persistence, created, func(st *ateapipb.ActorStatus) {
			st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: previous.String(), ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}
		})
		uploaded := durableCopyOf(mustActorSnapshotURI(t, tmpl, created, pauseSnapshot).String())
		objects.PutSnapshot(t, mustActorSnapshotURI(t, tmpl, created, pauseSnapshot), "manifest.json", "memory.zst")
		return w, persistence, objects, actor, uploaded
	}

	t.Run("records the copy and releases the replaced snapshot", func(t *testing.T) {
		ctx := context.Background()
		w, _, objects, actor, uploaded := seed(t)
		previous := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri()

		if err := w.recordDurablePauseCopy(ctx, actorRef, uploaded); err != nil {
			t.Fatalf("recordDurablePauseCopy: %v", err)
		}
		stored, err := w.store.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetExternalSnapshot(); got.GetSnapshotUri() != uploaded.GetSnapshotUri() || got.GetSourceLocalSnapshotName() != pauseSnapshot {
			t.Errorf("ExternalSnapshot = %v, want the uploaded copy %v", got, uploaded)
		}
		if stored.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED || stored.GetStatus().GetLocalSnapshotInfo().GetSnapshotName() != pauseSnapshot {
			t.Errorf("actor = %v, want still PAUSED on its local snapshot", stored.GetStatus())
		}
		if durablePauseCopy(stored) == nil {
			t.Error("durablePauseCopy = nil after recording the upload")
		}
		if got := objects.Prefix(t, mustParsePrefix(t, previous)); len(got) != 0 {
			t.Errorf("replaced snapshot still holds %v, want released", got)
		}
		if got := objects.Prefix(t, mustParsePrefix(t, uploaded.GetSnapshotUri())); len(got) == 0 {
			t.Error("the uploaded copy was released, want kept")
		}
	})

	t.Run("discards an upload the actor moved on from", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		previous := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri()
		// The actor resumed while the upload ran: RUNNING, its local snapshot
		// record stale, the store one version ahead of what the upload read.
		mustUpdateActorStatus(t, ctx, persistence, actor, func(st *ateapipb.ActorStatus) {
			st.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
		})

		if err := w.recordDurablePauseCopy(ctx, actorRef, uploaded); err != nil {
			t.Fatalf("recordDurablePauseCopy: %v", err)
		}
		stored, err := w.store.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != previous {
			t.Errorf("ExternalSnapshot = %q, want the previous %q untouched", got, previous)
		}
		if got := objects.Prefix(t, mustParsePrefix(t, uploaded.GetSnapshotUri())); len(got) != 0 {
			t.Errorf("stale upload still holds %v, want discarded", got)
		}
		if got := objects.Prefix(t, mustParsePrefix(t, previous)); len(got) == 0 {
			t.Error("the previous snapshot was released although the upload was discarded")
		}
	})

	t.Run("waits for a workflow holding the actor's lease", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		held, err := persistence.AcquireLease(ctx, actorLeaseKey(actorRef))
		if err != nil {
			t.Fatalf("AcquireLease: %v", err)
		}

		err = w.recordDurablePauseCopy(ctx, actorRef, uploaded)
		if got := status.Code(err); got != codes.Aborted {
			t.Fatalf("recordDurablePauseCopy under a held lease = %v, want Aborted (retried later)", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if stored.GetMetadata().GetVersion() != actor.GetMetadata().GetVersion() {
			t.Errorf("actor written while the lease was held: version %d, want %d", stored.GetMetadata().GetVersion(), actor.GetMetadata().GetVersion())
		}
		if got := objects.Prefix(t, mustParsePrefix(t, uploaded.GetSnapshotUri())); len(got) == 0 {
			t.Error("the upload was discarded although it is only deferred")
		}
		held.Close()

		if err := w.recordDurablePauseCopy(ctx, actorRef, uploaded); err != nil {
			t.Fatalf("recordDurablePauseCopy after the lease was released: %v", err)
		}
		stored, err = persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if durablePauseCopy(stored) == nil {
			t.Error("durablePauseCopy = nil after the lease was released")
		}
	})

	t.Run("leaves an upload another replica recorded", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		recorded := mustUpdateActorStatus(t, ctx, persistence, actor, func(st *ateapipb.ActorStatus) {
			st.ExternalSnapshot = uploaded
		})

		if err := w.recordDurablePauseCopy(ctx, actorRef, uploaded); err != nil {
			t.Fatalf("recordDurablePauseCopy: %v", err)
		}
		stored, err := w.store.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if stored.GetMetadata().GetVersion() != recorded.GetMetadata().GetVersion() {
			t.Errorf("actor version = %d, want %d (no second write)", stored.GetMetadata().GetVersion(), recorded.GetMetadata().GetVersion())
		}
		if got := objects.Prefix(t, mustParsePrefix(t, uploaded.GetSnapshotUri())); len(got) == 0 {
			t.Error("the recorded copy was discarded")
		}
	})
}

// TestEnsureMarkedSuspending_AdoptsDurablePauseCopy verifies a suspend of a
// paused actor whose pause snapshot already has a durable copy mints no
// destination: nothing will be uploaded, finalize keeps the copy. A running
// actor carrying the same, stale, local snapshot record still checkpoints.
func TestEnsureMarkedSuspending_AdoptsDurablePauseCopy(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{SnapshotsConfig: &ateapipb.SnapshotsConfig{StorageLocation: testStorageLocation}}
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}

	t.Run("paused with a durable copy", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence}
		st := pausedOn("node1")
		st.ExternalSnapshot = durableCopyOf(uri)
		actor := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
			Status:   st,
		})

		marked, err := w.ensureMarkedSuspending(ctx, actorRef, actor, tmpl)
		if err != nil {
			t.Fatalf("ensureMarkedSuspending: %v", err)
		}
		if marked.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
			t.Errorf("state = %v, want SUSPENDING", marked.GetStatus().GetState())
		}
		if got := marked.GetStatus().GetInProgressSnapshotUri(); got != "" {
			t.Errorf("InProgressSnapshotUri = %q, want none: the durable copy is committed as is", got)
		}
	})

	t.Run("paused with a durable copy of another scope", func(t *testing.T) {
		// A Full pause copy under a Data commit is not committed as it is: the
		// suspend uploads from the node, narrowing the snapshot to the commit
		// scope, as it does for a pause without a copy.
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence}
		narrowing := &ateapipb.ActorTemplate{SnapshotsConfig: &ateapipb.SnapshotsConfig{
			StorageLocation: testStorageLocation,
			OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			OnCommit:        ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
		}}
		st := pausedOn("node1")
		st.LocalSnapshotInfo.ContentScope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
		st.ExternalSnapshot = durableCopyOf(uri)
		st.ExternalSnapshot.ContentScope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
		actor := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
			Status:   st,
		})

		marked, err := w.ensureMarkedSuspending(ctx, actorRef, actor, narrowing)
		if err != nil {
			t.Fatalf("ensureMarkedSuspending: %v", err)
		}
		if got := marked.GetStatus().GetInProgressSnapshotUri(); got == "" {
			t.Error("InProgressSnapshotUri empty, want a fresh destination: a copy of another scope is not committed as is")
		}
	})

	t.Run("running with a stale local snapshot record", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence}
		st := pausedOn("node1")
		st.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
		st.ExternalSnapshot = durableCopyOf(uri)
		st.WorkerAssignment = &ateapipb.WorkerAssignment{WorkerNamespace: "ns", WorkerPool: "pool", WorkerPod: "pod-1"}
		actor := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
			Status:   st,
		})

		marked, err := w.ensureMarkedSuspending(ctx, actorRef, actor, tmpl)
		if err != nil {
			t.Fatalf("ensureMarkedSuspending: %v", err)
		}
		if got := marked.GetStatus().GetInProgressSnapshotUri(); got == "" {
			t.Error("InProgressSnapshotUri empty for a running actor, want a fresh destination")
		}
	})
}

// TestEnsurePausedSnapshotUploaded_AdoptsDurablePauseCopy verifies the upload
// step of a paused-origin suspend is a no-op when the pause snapshot already
// has a durable copy — with no atelet on the node at all, since the node's
// absence is what the copy is for.
func TestEnsurePausedSnapshotUploaded_AdoptsDurablePauseCopy(t *testing.T) {
	ctx := context.Background()
	persistence := newTestPersistence(t)
	w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer()}
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}

	st := pausedOn("node1")
	st.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDING
	st.ExternalSnapshot = durableCopyOf(uri)
	actor := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: actorRef.Atespace, Name: actorRef.Name},
		Status:   st,
	})

	scope, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, actor, &ateapipb.ActorTemplate{})
	if err != nil {
		t.Fatalf("ensurePausedSnapshotUploaded = %v, want nil: the copy is committed without the node", err)
	}
	if scope != ateattr.SnapshotScopeFull {
		t.Errorf("wire scope = %q, want %q (the copy's)", scope, ateattr.SnapshotScopeFull)
	}
	stored, err := persistence.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if stored.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
		t.Errorf("state = %v, want SUSPENDING (not crashed)", stored.GetStatus().GetState())
	}
}

// TestSuspendActor_PausedWithDurableCopyCommitsIt runs the whole suspend of a
// paused actor whose pause snapshot has a durable copy: no atelet is
// involved, the copy stays the external snapshot, the node pinning ends.
func TestSuspendActor_PausedWithDurableCopyCommitsIt(t *testing.T) {
	ctx := context.Background()
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	w := newTestActorWorkflow(t, st, "ns", "tmpl1")
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "id1"}

	seedWorkflowActor(t, ctx, st, actorRef, "ns", "tmpl1", ateapipb.ActorState_ACTOR_STATE_PAUSED, func(a *ateapipb.Actor) {
		a.Status = pausedOn("node1")
	})
	created, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	tmpl, err := st.GetActorTemplate(ctx, resources.ActorTemplateRef{Atespace: "ns", Name: "tmpl1"})
	if err != nil {
		t.Fatalf("GetActorTemplate: %v", err)
	}
	copyURI := mustActorSnapshotURI(t, tmpl, created, pauseSnapshot)
	objects := w.objectStore.(*objectstoretest.Fake)
	objects.PutSnapshot(t, copyURI, "manifest.json", "memory.zst")
	mustUpdateActorStatus(t, ctx, st, created, func(status *ateapipb.ActorStatus) {
		status.ExternalSnapshot = durableCopyOf(copyURI.String())
	})

	suspended, err := w.SuspendActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("SuspendActor: %v", err)
	}
	if suspended.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("state = %v, want SUSPENDED", suspended.GetStatus().GetState())
	}
	if got := suspended.GetStatus().GetExternalSnapshot(); got.GetSnapshotUri() != copyURI.String() || got.GetSourceLocalSnapshotName() != pauseSnapshot {
		t.Errorf("ExternalSnapshot = %v, want the durable copy %s kept", got, copyURI)
	}
	if suspended.GetStatus().GetLocalSnapshotInfo() != nil {
		t.Errorf("LocalSnapshotInfo = %v, want cleared", suspended.GetStatus().GetLocalSnapshotInfo())
	}
	if got := objects.Snapshot(t, copyURI); len(got) == 0 {
		t.Error("the durable copy was released by the suspend that committed it")
	}
}

// TestEnsureAteletRestored_RefusesPlacementOffSnapshotNodeWithoutCopy pins
// the guard behind the scheduler's node constraint: a paused actor whose
// snapshot exists only on its node, placed on another node, is refused before
// any atelet is dialed — the alternative would restore an older snapshot.
func TestEnsureAteletRestored_RefusesPlacementOffSnapshotNodeWithoutCopy(t *testing.T) {
	ctx := context.Background()
	w := &ActorWorkflow{}
	st := pausedOn("node1")
	st.State = ateapipb.ActorState_ACTOR_STATE_RESUMING
	st.WorkerAssignment = &ateapipb.WorkerAssignment{Worker: &ateapipb.ObjectRef{Name: "w2"}, WorkerNamespace: "ns", WorkerPool: "pool", WorkerPod: "pod-2"}
	actor := &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "actor-1"}, Status: st}
	worker := &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: "w2"}, NodeName: "node2"}

	_, err := w.ensureAteletRestored(ctx, resources.ActorRef{Atespace: "team-a", Name: "actor-1"}, actor, &ateapipb.ActorTemplate{}, resumeSnapshotSource{}, worker)
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("ensureAteletRestored = %v, want FailedPrecondition", err)
	}
}

// mustParsePrefix returns the storage prefix of the snapshot at uri.
func mustParsePrefix(t *testing.T, uri string) resources.StoragePrefix {
	t.Helper()
	parsed, err := resources.ParseSnapshotURI(uri)
	if err != nil {
		t.Fatalf("ParseSnapshotURI(%q): %v", uri, err)
	}
	return parsed.Prefix()
}
