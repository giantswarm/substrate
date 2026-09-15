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
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/scheduling"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/workercache"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// nodeListerOf returns a Node lister that knows exactly the named nodes.
func nodeListerOf(t *testing.T, names ...string) corev1listers.NodeLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, name := range names {
		if err := indexer.Add(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
			t.Fatalf("index node %q: %v", name, err)
		}
	}
	return corev1listers.NewNodeLister(indexer)
}

// TestLocalSnapshotNodes_DropsEmptyEntries guards the read side of the
// empty-node poison: a pause finalized without a node name once recorded
// [""], and no worker has an empty node name, so kept as a restriction the
// entry excluded every worker for good. The scheduling constraints are built
// from the same reading.
func TestLocalSnapshotNodes_DropsEmptyEntries(t *testing.T) {
	tests := []struct {
		name     string
		recorded []string
		want     []string
	}{
		{name: "no local snapshot", recorded: nil, want: nil},
		{name: "a node", recorded: []string{"node1"}, want: []string{"node1"}},
		{name: "only an empty entry", recorded: []string{""}, want: nil},
		{name: "an empty entry beside a node", recorded: []string{"", "node1"}, want: []string{"node1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actor := &ateapipb.Actor{}
			if tc.recorded != nil {
				actor.Status = &ateapipb.ActorStatus{LocalSnapshotInfo: &ateapipb.LocalSnapshotInfo{NodeVmsWithLocalSnapshots: tc.recorded}}
			}
			if got := localSnapshotNodes(actor); !slices.Equal(got, tc.want) {
				t.Errorf("localSnapshotNodes() = %v, want %v", got, tc.want)
			}
			constraints, err := schedulingConstraints(actor, &ateapipb.ActorTemplate{})
			if err != nil {
				t.Fatalf("schedulingConstraints: %v", err)
			}
			if !slices.Equal(constraints.RequiredNodes, tc.want) {
				t.Errorf("Constraints.RequiredNodes = %v, want %v", constraints.RequiredNodes, tc.want)
			}
		})
	}
}

// seedPausedActor stores a PAUSED actor of team-a/tmpl1 whose local snapshot
// is recorded on node1; mutate adjusts the status before it is stored.
func seedPausedActor(t *testing.T, ctx context.Context, persistence store.Interface, mutate func(*ateapipb.ActorStatus)) *ateapipb.Actor {
	t.Helper()
	st := &ateapipb.ActorStatus{
		State:             ateapipb.ActorState_ACTOR_STATE_PAUSED,
		LocalSnapshotInfo: &ateapipb.LocalSnapshotInfo{SnapshotName: "snap", NodeVmsWithLocalSnapshots: []string{"node1"}},
	}
	if mutate != nil {
		mutate(st)
	}
	return storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "paused"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "tmpl1"},
		Status:        st,
	})
}

// activeWorker is an ACTIVE worker of pool worker-ns/pool on node with room
// for one actor.
func activeWorker(pod, class, node string) *ateapipb.Worker {
	return &ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: testWorkerUID(pod)},
		WorkerNamespace: "worker-ns",
		WorkerPool:      "pool",
		WorkerPod:       pod,
		WorkerPodUid:    testWorkerUID(pod),
		SandboxClass:    class,
		NodeName:        node,
		Status:          &ateapipb.WorkerStatus{State: ateapipb.WorkerState_WORKER_STATE_ACTIVE, Capacity: &ateapipb.WorkerResources{Actors: 1}},
	}
}

// startedWorkerCache seeds the fleet into persistence, places a resident
// actor on each of the busy pods, and returns a started worker cache over it,
// stopped when the test ends. The residents are seeded before the cache
// starts so its initial list already carries their allocation.
func startedWorkerCache(t *testing.T, ctx context.Context, persistence store.Interface, fleet []*ateapipb.Worker, busy ...string) *workercache.Cache {
	t.Helper()
	for _, w := range fleet {
		if _, err := persistence.CreateWorker(ctx, w); err != nil {
			t.Fatalf("CreateWorker(%s): %v", w.GetWorkerPod(), err)
		}
	}
	for _, pod := range busy {
		seedAssignment(t, persistence, testWorkerUID(pod), &ateapipb.ActorAssignment{
			Actor:    &ateapipb.ObjectRef{Atespace: "team-b", Name: "resident"},
			ActorUid: "resident-uid",
		})
	}
	cacheCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	wc := workercache.New(persistence, time.Minute)
	if err := wc.Start(cacheCtx); err != nil {
		t.Fatalf("workercache.Start: %v", err)
	}
	return wc
}

// crashCounterReader registers ate.actor.crashes on a fresh reader, so a
// test can assert what the assignment counted.
func crashCounterReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	if err := RegisterActorCrashes(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")); err != nil {
		t.Fatalf("RegisterActorCrashes: %v", err)
	}
	return reader
}

// TestAssignWorkerAttempt_PausedActorLocalSnapshotNode covers the assignment
// of a PAUSED actor, whose local snapshot pins it to node1, when no worker on
// node1 takes it. Only a node that has left the cluster -- no ACTIVE worker of
// any pool reports it and the Node object is gone -- loses the snapshot; then
// the actor without a durable snapshot is crashed with LOCAL_SNAPSHOT_GONE
// and the caller told through the crash directive, whatever earlier suspend's
// snapshot it holds; one whose pause has a durable copy is not pinned at all
// and lands elsewhere. Everything short of that stays
// ResourceExhausted, which the router parks on, with the node named.
func TestAssignWorkerAttempt_PausedActorLocalSnapshotNode(t *testing.T) {
	tests := []struct {
		name string
		// fleet is the worker fleet; busy names the pods already hosting
		// another actor.
		fleet []*ateapipb.Worker
		busy  []string
		// nodes are the Node objects the lister knows; noLister leaves the
		// workflow without node knowledge.
		nodes    []string
		noLister bool
		// external gives the actor the external snapshot of an earlier suspend
		// beside the local one; durableCopy the uploaded copy of the local one.
		external    bool
		durableCopy bool
		wantCode    codes.Code
		wantMessage string
		wantState   ateapipb.ActorState
		wantCrashed bool
	}{
		{
			name:        "eligible workers on the snapshot's node are busy",
			fleet:       []*ateapipb.Worker{activeWorker("w1", "gvisor", "node1"), activeWorker("w2", "gvisor", "node2")},
			busy:        []string{"w1"},
			wantCode:    codes.ResourceExhausted,
			wantMessage: "on node(s) [node1] but all eligible workers on those nodes are busy",
			wantState:   ateapipb.ActorState_ACTOR_STATE_PAUSED,
		},
		{
			// The lister knows no node1: a worker reporting it is proof enough
			// that the node is there.
			name:        "another pool's worker on the node keeps it present",
			fleet:       []*ateapipb.Worker{activeWorker("w1", "microvm", "node1"), activeWorker("w2", "gvisor", "node2")},
			wantCode:    codes.ResourceExhausted,
			wantMessage: "on node(s) [node1] but no eligible workers exist on those nodes",
			wantState:   ateapipb.ActorState_ACTOR_STATE_PAUSED,
		},
		{
			name:        "the node is present but hosts no worker",
			fleet:       []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")},
			nodes:       []string{"node1"},
			wantCode:    codes.ResourceExhausted,
			wantMessage: "on node(s) [node1] but no workers exist on those nodes",
			wantState:   ateapipb.ActorState_ACTOR_STATE_PAUSED,
		},
		{
			name:        "no node knowledge keeps the actor waiting",
			fleet:       []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")},
			noLister:    true,
			wantCode:    codes.ResourceExhausted,
			wantMessage: "on node(s) [node1] but no workers exist on those nodes",
			wantState:   ateapipb.ActorState_ACTOR_STATE_PAUSED,
		},
		{
			name:        "the node is gone and there is no durable snapshot: crashed",
			fleet:       []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")},
			wantCode:    codes.DataLoss,
			wantMessage: `crashed: its local snapshot "snap" is on node(s) [node1], which no longer exist in the cluster, and it has no durable copy of it`,
			wantState:   ateapipb.ActorState_ACTOR_STATE_CRASHED,
			wantCrashed: true,
		},
		{
			// An earlier suspend's snapshot is older state: restoring it would
			// silently revert the actor, so it does not save the pause.
			name:        "the node is gone and only an earlier suspend's snapshot remains: crashed all the same",
			fleet:       []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")},
			external:    true,
			wantCode:    codes.DataLoss,
			wantMessage: `crashed: its local snapshot "snap" is on node(s) [node1], which no longer exist in the cluster, and it has no durable copy of it`,
			wantState:   ateapipb.ActorState_ACTOR_STATE_CRASHED,
			wantCrashed: true,
		},
		{
			// With the pause uploaded the node is only preferred: the free
			// worker elsewhere takes the actor.
			name:        "the node is gone but the pause has a durable copy: placed elsewhere",
			fleet:       []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")},
			durableCopy: true,
			wantCode:    codes.OK,
			wantState:   ateapipb.ActorState_ACTOR_STATE_RESUMING,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			reader := crashCounterReader(t)
			persistence := newTestPersistence(t)
			actorRef := resources.ActorRef{Atespace: "team-a", Name: "paused"}
			actor := seedPausedActor(t, ctx, persistence, func(st *ateapipb.ActorStatus) {
				if tc.external {
					st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: someActorSnapshotURI(t, testStorageLocation, "team-a", "durable")}
				}
				if tc.durableCopy {
					st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: someActorSnapshotURI(t, testStorageLocation, "team-a", "snap"), SourceLocalSnapshotName: "snap"}
				}
			})
			wc := startedWorkerCache(t, ctx, persistence, tc.fleet, tc.busy...)
			w := &ActorWorkflow{store: persistence, workerCache: wc, scheduler: scheduling.New(wc)}
			if !tc.noLister {
				w.nodeLister = nodeListerOf(t, tc.nodes...)
			}
			tmpl := &ateapipb.ActorTemplate{SandboxConfig: &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR}}

			_, _, err := w.assignWorkerAttempt(ctx, actorRef, actor, tmpl)
			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("assignWorkerAttempt() error = %v, want code %v", err, tc.wantCode)
			}
			if msg := status.Convert(err).Message(); !strings.Contains(msg, tc.wantMessage) {
				t.Errorf("message = %q, want it to contain %q", msg, tc.wantMessage)
			}
			if tc.wantCode == codes.DataLoss {
				if got := ateerrors.ExtractReason(err); got != string(ateerrors.ReasonLocalSnapshotGone) {
					t.Errorf("reason = %q, want %q", got, ateerrors.ReasonLocalSnapshotGone)
				}
			}
			if got := ateerrors.ActorCrashRequested(err); got != tc.wantCrashed {
				t.Errorf("ActorCrashRequested(err) = %v, want %v (err: %v)", got, tc.wantCrashed, err)
			}

			stored, err := persistence.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor: %v", err)
			}
			if got := stored.GetStatus().GetState(); got != tc.wantState {
				t.Errorf("stored state = %v, want %v", got, tc.wantState)
			}
			if stored.GetStatus().GetLocalSnapshotInfo() == nil {
				t.Error("stored actor lost its LocalSnapshotInfo; the record is kept for debugging")
			}
			if tc.wantCrashed {
				// A paused actor holds no worker: the crash names the template,
				// no pool and no sandbox class.
				assertCrashMetricDatapoint(t, reader, ateattr.OperationResume, ateattr.ReasonLocalSnapshotGone, "team-a", "tmpl1", "", ateattr.SandboxClassUnknown, 1)
			} else {
				assertNoCrashMetricDatapoint(t, reader)
			}
		})
	}
}

// TestResumeActor_PausedOnGoneNodeCrashes runs the whole Resume workflow
// over a PAUSED actor whose node left the cluster: the caller gets DataLoss
// with the crash directive at once, the actor is CRASHED, and a second
// resume is refused on the state, not parked.
func TestResumeActor_PausedOnGoneNodeCrashes(t *testing.T) {
	ctx := context.Background()
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	w := newTestActorWorkflow(t, st, "team-a", "tmpl1")
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "paused"}
	seedPausedActor(t, ctx, st, nil)
	wc := startedWorkerCache(t, ctx, st, []*ateapipb.Worker{activeWorker("w2", "gvisor", "node2")})
	w.workerCache, w.scheduler, w.nodeLister = wc, scheduling.New(wc), nodeListerOf(t)

	_, _, err := w.ResumeActor(ctx, actorRef)
	if got := status.Code(err); got != codes.DataLoss {
		t.Fatalf("ResumeActor error = %v, want DataLoss", err)
	}
	if !ateerrors.ActorCrashRequested(err) {
		t.Errorf("ResumeActor error carries no crash directive: %v", err)
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Fatalf("stored state = %v, want CRASHED", got)
	}

	_, _, err = w.ResumeActor(ctx, actorRef)
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("second ResumeActor error = %v, want FailedPrecondition on the CRASHED state", err)
	}
}

// TestEnsurePausedSnapshotUploaded_NodeGone covers the suspend of a PAUSED
// actor whose node left the cluster: there is no atelet to ask for the upload
// and never will be. Without a durable copy of the pause the actor is crashed
// (an earlier suspend's snapshot does not count), with one the copy is
// committed; a node that is still there keeps the step retryable,
// and an empty node entry beside the real one is ignored.
func TestEnsurePausedSnapshotUploaded_NodeGone(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{SnapshotsConfig: &ateapipb.SnapshotsConfig{StorageLocation: "gs://snapshots"}}
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	seed := func(t *testing.T, ctx context.Context, persistence store.Interface, nodes []string, external string) *ateapipb.Actor {
		t.Helper()
		st := &ateapipb.ActorStatus{
			State:                 ateapipb.ActorState_ACTOR_STATE_SUSPENDING,
			InProgressSnapshotUri: "gs://snapshots/atespaces/team-a/actors/actor-1/snapshots/snap-dest",
			LocalSnapshotInfo:     &ateapipb.LocalSnapshotInfo{SnapshotName: "snap", NodeVmsWithLocalSnapshots: nodes},
		}
		if external != "" {
			st.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: external}
		}
		return storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "actor-1"},
			Status:   st,
		})
	}

	t.Run("no durable snapshot: crashed", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer(), nodeLister: nodeListerOf(t)}
		created := seed(t, ctx, persistence, []string{"node1"}, "")

		_, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, created, tmpl)
		if got := status.Code(err); got != codes.DataLoss || !ateerrors.ActorCrashRequested(err) {
			t.Fatalf("ensurePausedSnapshotUploaded = %v, want DataLoss with the crash directive", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
			t.Errorf("state = %v, want CRASHED", got)
		}
	})

	t.Run("only an earlier suspend's snapshot remains: crashed all the same", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer(), nodeLister: nodeListerOf(t)}
		created := seed(t, ctx, persistence, []string{"node1"}, someActorSnapshotURI(t, testStorageLocation, "team-a", "durable"))

		_, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, created, tmpl)
		if got := status.Code(err); got != codes.DataLoss || !ateerrors.ActorCrashRequested(err) {
			t.Fatalf("ensurePausedSnapshotUploaded = %v, want DataLoss with the crash directive: an older snapshot does not save the pause", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
			t.Errorf("state = %v, want CRASHED", got)
		}
	})

	t.Run("the pause has a durable copy: committed without the node", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer(), nodeLister: nodeListerOf(t)}
		copyURI := someActorSnapshotURI(t, testStorageLocation, "team-a", "snap")
		created := storetest.MustCreateActor(t, ctx, persistence, &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "actor-1"},
			Status: &ateapipb.ActorStatus{
				State:             ateapipb.ActorState_ACTOR_STATE_SUSPENDING,
				LocalSnapshotInfo: &ateapipb.LocalSnapshotInfo{SnapshotName: "snap", NodeVmsWithLocalSnapshots: []string{"node1"}},
				ExternalSnapshot:  &ateapipb.ExternalSnapshot{SnapshotUri: copyURI, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, SourceLocalSnapshotName: "snap"},
			},
		})

		if _, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, created, tmpl); err != nil {
			t.Fatalf("ensurePausedSnapshotUploaded = %v, want nil: the copy is committed without the node", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED && got != ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
			t.Errorf("state = %v, want SUSPENDING", got)
		}
		if got := stored.GetStatus().GetState(); got == ateapipb.ActorState_ACTOR_STATE_CRASHED {
			t.Errorf("state = CRASHED, want SUSPENDING: the durable copy commits")
		}
	})

	t.Run("the node is present: stays retryable", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer(), nodeLister: nodeListerOf(t, "node1")}
		created := seed(t, ctx, persistence, []string{"node1"}, "")

		_, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, created, tmpl)
		if !errors.Is(err, ErrNoAteletOnNode) {
			t.Fatalf("ensurePausedSnapshotUploaded = %v, want ErrNoAteletOnNode", err)
		}
		stored, err := persistence.GetActor(ctx, actorRef)
		if err != nil {
			t.Fatalf("GetActor: %v", err)
		}
		if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
			t.Errorf("state = %v, want SUSPENDING (retryable, not crashed)", got)
		}
	})

	t.Run("an empty node entry beside the real one is ignored", func(t *testing.T) {
		ctx := context.Background()
		persistence := newTestPersistence(t)
		w := &ActorWorkflow{store: persistence, dialer: newDanglingDialer(), nodeLister: nodeListerOf(t, "node1")}
		created := seed(t, ctx, persistence, []string{"", "node1"}, "")

		_, err := w.ensurePausedSnapshotUploaded(ctx, actorRef, created, tmpl)
		if !errors.Is(err, ErrNoAteletOnNode) || !strings.Contains(err.Error(), `"node1"`) {
			t.Fatalf("ensurePausedSnapshotUploaded = %v, want ErrNoAteletOnNode for node1", err)
		}
	})
}
