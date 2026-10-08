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
	"sync"
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
		LocalSnapshot: &ateapipb.LocalSnapshot{
			SnapshotName:              pauseSnapshot,
			NodeVmsWithLocalSnapshots: []string{node},
			ContentScope:              ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
		},
	}
}

// durableCopyOf is the durable copy the background upload of the pause
// snapshot records beside it.
func durableCopyOf(uri string) *ateapipb.ExternalSnapshot {
	return &ateapipb.ExternalSnapshot{
		SnapshotUri:             uri,
		ContentScope:            ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
		SourceLocalSnapshotName: pauseSnapshot,
	}
}

// TestDurablePauseCopy pins what counts as the durable copy of a pause: only
// the copy recorded beside the local snapshot and uploaded from it. Anything
// else — the external snapshot, even one a suspend committed from an earlier
// pause's copy, or a copy of a previous pause — is older state and must never
// stand in for the pause.
func TestDurablePauseCopy(t *testing.T) {
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	tests := []struct {
		name   string
		status *ateapipb.ActorStatus
		want   bool
	}{
		{"no local snapshot", &ateapipb.ActorStatus{ExternalSnapshot: durableCopyOf(uri)}, false},
		{"no durable copy", pausedOn("node1"), false},
		{"external snapshot from an earlier suspend", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri}
			return st
		}(), false},
		{"external snapshot naming the local snapshot", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.ExternalSnapshot = durableCopyOf(uri)
			return st
		}(), false},
		{"copy of an earlier pause", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
			st.LocalSnapshot.DurableCopy.SourceLocalSnapshotName = "pause-snap-0"
			return st
		}(), false},
		{"the upload of the pause snapshot", func() *ateapipb.ActorStatus {
			st := pausedOn("node1")
			st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
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
			st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
			return st
		}()}, false},
		{"paused with no node recorded", &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{UpdateTime: old}, Status: &ateapipb.ActorStatus{
			State:         ateapipb.ActorState_ACTOR_STATE_PAUSED,
			LocalSnapshot: &ateapipb.LocalSnapshot{SnapshotName: pauseSnapshot},
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
		st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
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
// the record: recorded beside the local snapshot, the external snapshot of the
// last suspend untouched, while the actor is still paused on that snapshot; discarded when the actor moved on before
// the upload finished; left alone when another replica already recorded it;
// deferred while a lifecycle workflow holds the actor's lease, so a resume
// that read the record never meets a version it did not see.
func TestRecordDurablePauseCopy(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{SnapshotConfig: &ateapipb.SnapshotConfig{StorageLocation: testStorageLocation}}
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

	t.Run("records the copy and keeps the suspend's snapshot", func(t *testing.T) {
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
		if got := stored.GetStatus().GetLocalSnapshot().GetDurableCopy(); got.GetSnapshotUri() != uploaded.GetSnapshotUri() || got.GetSourceLocalSnapshotName() != pauseSnapshot {
			t.Errorf("DurableCopy = %v, want the uploaded copy %v", got, uploaded)
		}
		if got := stored.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != previous {
			t.Errorf("ExternalSnapshot = %q, want the suspend's %q untouched", got, previous)
		}
		if stored.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED || stored.GetStatus().GetLocalSnapshot().GetSnapshotName() != pauseSnapshot {
			t.Errorf("actor = %v, want still PAUSED on its local snapshot", stored.GetStatus())
		}
		if durablePauseCopy(stored) == nil {
			t.Error("durablePauseCopy = nil after recording the upload")
		}
		if got := objects.Prefix(t, mustParsePrefix(t, previous)); len(got) == 0 {
			t.Error("the suspend's snapshot was released, want kept for a revert")
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

	t.Run("discards a stale upload without holding the actor's lease", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		mustUpdateActorStatus(t, ctx, persistence, actor, func(st *ateapipb.ActorStatus) {
			st.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
		})
		slow := &blockingDeletes{Fake: objects, deleting: make(chan struct{}), unblock: make(chan struct{})}
		w.objectStore = slow

		recorded := make(chan error, 1)
		go func() { recorded <- w.recordDurablePauseCopy(ctx, actorRef, uploaded) }()
		<-slow.deleting

		// The client's next operation on the actor: it must not wait for the
		// object store, and must not turn into Aborted because of it.
		_, lease, err := w.acquireActorLease(ctx, actorRef, ateattr.OperationResume)
		close(slow.unblock)
		if err != nil {
			t.Fatalf("acquireActorLease while the stale upload is deleted = %v, want the lease", err)
		}
		lease.Close()
		if err := <-recorded; err != nil {
			t.Fatalf("recordDurablePauseCopy: %v", err)
		}
		if got := objects.Prefix(t, mustParsePrefix(t, uploaded.GetSnapshotUri())); len(got) != 0 {
			t.Errorf("stale upload still holds %v, want discarded", got)
		}
	})

	t.Run("waits for a workflow holding the actor's lease", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		held, err := persistence.AcquireLease(ctx, actorLeaseKey(actorRef), ateattr.OperationResume)
		if err != nil {
			t.Fatalf("AcquireLease: %v", err)
		}

		recorded := make(chan error, 1)
		go func() { recorded <- w.recordDurablePauseCopy(ctx, actorRef, uploaded) }()
		select {
		case err := <-recorded:
			t.Fatalf("recordDurablePauseCopy returned %v while the lease is held, want to wait for the holder", err)
		case <-time.After(3 * pauseCommitRetryInterval):
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

		if err := <-recorded; err != nil {
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

	t.Run("the actor's next operation is not Aborted by a slow commit", func(t *testing.T) {
		// The client's pause or suspend right after a resume from PAUSED: the
		// commit of the pause's upload is in flight, its store round trips
		// slower than any wait a client has for the actor's lease. The
		// operation takes the lease at once; the commit then finds the actor
		// under it, waits, and records the copy once the operation is done.
		ctx := context.Background()
		w, persistence, _, _, uploaded := seed(t)
		w.store = slowReads{Interface: persistence, delay: 400 * time.Millisecond}

		started := make(chan struct{})
		recorded := make(chan error, 1)
		go func() {
			close(started)
			recorded <- w.recordDurablePauseCopy(ctx, actorRef, uploaded)
		}()
		<-started
		time.Sleep(50 * time.Millisecond)

		start := time.Now()
		_, lease, err := w.acquireActorLease(ctx, actorRef, ateattr.OperationPause)
		if err != nil {
			t.Fatalf("acquireActorLease while the commit is in flight = %v, want the lease", err)
		}
		if waited := time.Since(start); waited > 200*time.Millisecond {
			t.Errorf("the lease took %s, want at once: the commit holds none", waited)
		}
		// The operation runs for longer than the commit's wait and keeps
		// the actor paused, which is what the commit then records.
		time.Sleep(2 * pauseCommitRetryInterval)
		lease.Close()

		if err := <-recorded; err != nil {
			t.Fatalf("recordDurablePauseCopy: %v", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if durablePauseCopy(stored) == nil {
			t.Error("durablePauseCopy = nil after the operation released the actor")
		}
	})

	t.Run("leaves an upload another replica recorded", func(t *testing.T) {
		ctx := context.Background()
		w, persistence, objects, actor, uploaded := seed(t)
		recorded := mustUpdateActorStatus(t, ctx, persistence, actor, func(st *ateapipb.ActorStatus) {
			st.LocalSnapshot.DurableCopy = uploaded
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
	tmpl := &ateapipb.ActorTemplate{SnapshotConfig: &ateapipb.SnapshotConfig{StorageLocation: testStorageLocation}}
	uri := someActorSnapshotURI(t, testStorageLocation, "team-a", pauseSnapshot)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}

	t.Run("paused with a durable copy", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence}
		st := pausedOn("node1")
		st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
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
		narrowing := &ateapipb.ActorTemplate{SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation: testStorageLocation,
			OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			OnCommit:        ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
		}}
		st := pausedOn("node1")
		st.LocalSnapshot.ContentScope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
		st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
		st.LocalSnapshot.DurableCopy.ContentScope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
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
		st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
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
	st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
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
// involved, the copy becomes the external snapshot and the one it replaces is
// released, the node pinning ends.
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
	previousURI := mustActorSnapshotURI(t, tmpl, created, "suspend-snap-0")
	objects.PutSnapshot(t, previousURI, "manifest.json", "memory.zst")
	mustUpdateActorStatus(t, ctx, st, created, func(status *ateapipb.ActorStatus) {
		status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: previousURI.String()}
		status.LocalSnapshot.DurableCopy = durableCopyOf(copyURI.String())
	})

	suspended, err := w.SuspendActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("SuspendActor: %v", err)
	}
	if suspended.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("state = %v, want SUSPENDED", suspended.GetStatus().GetState())
	}
	if got := suspended.GetStatus().GetExternalSnapshot(); got.GetSnapshotUri() != copyURI.String() || got.GetSourceLocalSnapshotName() != pauseSnapshot {
		t.Errorf("ExternalSnapshot = %v, want the durable copy %s committed", got, copyURI)
	}
	if got := objects.Snapshot(t, previousURI); len(got) != 0 {
		t.Errorf("the snapshot the commit replaced still holds %v, want released", got)
	}
	if suspended.GetStatus().GetLocalSnapshot() != nil {
		t.Errorf("LocalSnapshot = %v, want cleared", suspended.GetStatus().GetLocalSnapshot())
	}
	if got := objects.Snapshot(t, copyURI); len(got) == 0 {
		t.Error("the durable copy was released by the suspend that committed it")
	}
}

// TestRevertActor_PausedWithDurableCopy runs a revert of a paused actor whose
// pause snapshot has a durable copy: the actor returns to the last suspend's
// external snapshot, which stays in storage, and the copy of the discarded
// pause is released.
func TestRevertActor_PausedWithDurableCopy(t *testing.T) {
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
	suspendURI := mustActorSnapshotURI(t, tmpl, created, "suspend-snap-0")
	copyURI := mustActorSnapshotURI(t, tmpl, created, pauseSnapshot)
	objects := w.objectStore.(*objectstoretest.Fake)
	objects.PutSnapshot(t, suspendURI, "manifest.json", "memory.zst")
	objects.PutSnapshot(t, copyURI, "manifest.json", "memory.zst")
	mustUpdateActorStatus(t, ctx, st, created, func(status *ateapipb.ActorStatus) {
		status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: suspendURI.String()}
		status.LocalSnapshot.DurableCopy = durableCopyOf(copyURI.String())
	})

	reverted, err := w.RevertActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("RevertActor: %v", err)
	}
	if got := reverted.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("state = %v, want SUSPENDED", got)
	}
	if got := reverted.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != suspendURI.String() {
		t.Errorf("ExternalSnapshot = %q, want the suspend's %q", got, suspendURI)
	}
	if got := objects.Snapshot(t, suspendURI); len(got) == 0 {
		t.Error("the suspend's snapshot was released, want kept")
	}
	if got := objects.Snapshot(t, copyURI); len(got) != 0 {
		t.Errorf("the discarded pause's copy still holds %v, want released", got)
	}
}

// TestReleaseDurablePauseCopy pins when a dropped copy is deleted: never
// while the stored record still names it, as its external snapshot or as the
// copy of the pause it kept.
func TestReleaseDurablePauseCopy(t *testing.T) {
	ctx := context.Background()
	tmpl := &ateapipb.ActorTemplate{SnapshotConfig: &ateapipb.SnapshotConfig{StorageLocation: testStorageLocation}}
	actor := &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "actor-1", Uid: "168e8c8b-4a1b-45a5-b1ac-d6597bd6f1ae"}, Status: pausedOn("node1")}
	copyURI := mustActorSnapshotURI(t, tmpl, actor, pauseSnapshot)
	actor.Status.LocalSnapshot.DurableCopy = durableCopyOf(copyURI.String())

	tests := []struct {
		name     string
		stored   *ateapipb.ActorStatus
		released bool
	}{
		{"the next pause replaced it", &ateapipb.ActorStatus{LocalSnapshot: &ateapipb.LocalSnapshot{SnapshotName: "pause-snap-2"}}, true},
		{"the record dropped the pause", &ateapipb.ActorStatus{}, true},
		{"a suspend committed it", &ateapipb.ActorStatus{ExternalSnapshot: durableCopyOf(copyURI.String())}, false},
		{"the record kept it", pausedOnWithCopy(copyURI.String()), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			persistence := newTestPersistence(t)
			w, objects := newFinalizeWorkflow(persistence)
			objects.PutSnapshot(t, copyURI, "manifest.json", "memory.zst")
			w.releaseDurablePauseCopy(ctx, actor, &ateapipb.Actor{Status: tc.stored})
			if gone := len(objects.Snapshot(t, copyURI)) == 0; gone != tc.released {
				t.Errorf("copy released = %t, want %t", gone, tc.released)
			}
		})
	}
}

// pausedOnWithCopy is pausedOn("node1") with the pause snapshot's durable copy.
func pausedOnWithCopy(uri string) *ateapipb.ActorStatus {
	st := pausedOn("node1")
	st.LocalSnapshot.DurableCopy = durableCopyOf(uri)
	return st
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

// slowReads is a store whose actor reads take delay, the way a loaded
// database answers.
type slowReads struct {
	store.Interface
	delay time.Duration
}

func (s slowReads) GetActor(ctx context.Context, actorRef resources.ActorRef) (*ateapipb.Actor, error) {
	time.Sleep(s.delay)
	return s.Interface.GetActor(ctx, actorRef)
}

// blockingDeletes is an object store whose deletes wait for unblock, the way
// deleting a large snapshot's objects takes as long as the object store does.
// deleting is closed when the first delete starts.
type blockingDeletes struct {
	*objectstoretest.Fake
	deleting chan struct{}
	unblock  chan struct{}
	once     sync.Once
}

func (b *blockingDeletes) Delete(ctx context.Context, bucket, object string) error {
	b.once.Do(func() { close(b.deleting) })
	select {
	case <-b.unblock:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.Fake.Delete(ctx, bucket, object)
}
