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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/scheduling"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/resources"
	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ateomSocketGone is the error atelet returns when the worker's ateom is not
// listening yet, as a rolling worker pool produces it.
var ateomSocketGone = status.Error(codes.Unknown, `ateom.RunWorkload: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial unix /run/ate/workers/w-1/ateom.sock: connect: no such file or directory"`)

// wakeupProbeCancelledCrash is the crash a resume records when the worker's
// call, and its wakeup probe with it, is cancelled while its pod goes away.
const wakeupProbeCancelledCrash = `resume failed: atelet Run: while calling ateom.RunWorkload: rpc error: code = Canceled desc = while waiting for container wakeup probe: wakeup probe cancelled for "kagent" after 1.2s (3 attempts, last error: connection refused): context canceled`

// sandboxNetworkCrash is the crash a resume records when the worker's sandbox
// did not take the actor's network, so its wakeup probe was refused.
const sandboxNetworkCrash = `resume failed: atelet Run: while calling ateom.RunWorkload: rpc error: code = Unknown desc = the sandbox did not take the actor's network: eth0 in its namespace still carries 169.254.17.2`

// runscKilled is the error a restore returns when its sandbox is killed: by
// the worker pod going away, or by the kernel for its memory.
var runscKilled = status.Error(codes.Internal, "while calling ateom.RestoreWorkload: rpc error: code = Internal desc = while starting pause container: while running `runsc start`: signal: killed")

// ateomGone is the error atelet returns for a call to an ateom whose pod went
// away.
var ateomGone = status.Error(codes.Internal, `failed calling ateom.TerminateWorkload: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial unix /run/ate/workers/w-1/ateom.sock: connect: connection refused"`)

func TestTransientResumeCrash(t *testing.T) {
	crashed := ateapipb.ActorState_ACTOR_STATE_CRASHED
	tests := []struct {
		name  string
		state ateapipb.ActorState
		crash string
		want  bool
	}{
		{"draining worker", crashed, "resume failed: " + crashMessageWorkerDraining, true},
		{"worker gone", crashed, "resume failed: " + crashMessageWorkerGone, true},
		{"worker reassigned", crashed, "resume failed: " + crashMessageWorkerReassigned, true},
		{"worker pod gone", crashed, "resume failed: " + crashMessageWorkerPodGone, true},
		{"ateom restarted", crashed, "resume failed: " + crashMessageAteomRestarted, true},
		{"ateom socket missing", crashed, "resume failed: " + ateletCrashMessage("Restore", ateomSocketGone), true},
		{"atelet without an IP", crashed, `resume failed: atelet Restore: selected atelet "ate-system/atelet-x" has no assigned IPs`, true},
		{"two atelet pods", crashed, `resume failed: atelet Restore: found 2 atelet pods on node "n-1", expected 1`, true},
		{"wakeup probe cancelled under a rolling worker", crashed, wakeupProbeCancelledCrash, true},
		{"sandbox did not take the network", crashed, sandboxNetworkCrash, true},
		{"wakeup probe refused by a sandbox that did not take the network", crashed, `resume failed: atelet Run: while calling ateom.RunWorkload: rpc error: code = Unknown desc = while waiting for container wakeup probe: wakeup probe for "counter" never returned 200 within 30s (18290 attempts, last error: connection refused); the sandbox did not take the actor's network: eth0 in its namespace still carries 169.254.17.2`, true},
		{"worker unreachable after the failure", crashed, "resume failed: atelet Restore: " + runscKilled.Error() + " (" + crashMessageWorkerUnreachable + ")", true},
		{"runsc killed on a draining worker", crashed, "resume failed: " + ateletCrashMessage("Restore", runscKilled) + " (" + crashMessageWorkerDraining + ")", true},
		{"runsc killed on a live worker", crashed, "resume failed: " + ateletCrashMessage("Restore", runscKilled), false},
		{"manifest unknown", crashed, "resume failed: atelet Restore: MANIFEST_UNKNOWN: manifest unknown", false},
		{"no runnable process", crashed, "resume failed: atelet Run: no runnable process in container", false},
		{"wakeup probe never answered", crashed, `resume failed: atelet Run: while calling ateom.RunWorkload: rpc error: code = Unknown desc = while waiting for container wakeup probe: wakeup probe for "kagent" never returned 200 within 2m0s (40 attempts, last error: connection refused); the sandbox holds the actor's network`, false},
		// The template decides whether a resume outlives the trust-bundle
		// guard (outlivedResumeCrash), not the crash alone.
		{"trust-bundle guard", crashed, trustBundleNamesCrash, false},
		{"budget exceeded", crashed, "resume failed: restore of actor team-a/a did not finish within its budget of 5m0s in 3 attempt(s)", false},
		// The actor ran after its snapshot: going back to it would lose work.
		{"running actor's pod gone", crashed, "suspend failed: " + crashMessageWorkerPodGone, false},
		{"crash without an operation", crashed, crashMessageWorkerPodGone, false},
		{"recovered since", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "resume failed: " + crashMessageWorkerDraining, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: tt.state, Crash: &ateapipb.ActorCrash{Message: tt.crash}}}
			if got := transientResumeCrash(actor); got != tt.want {
				t.Errorf("transientResumeCrash(%v, %q) = %v, want %v", tt.state, tt.crash, got, tt.want)
			}
		})
	}
}

// flakyAtelet fails the Restore calls with the errors queued in restoreErrs,
// one per call, or every one with failAlways, and answers the others and every
// Terminate and Checkpoint with success, unless terminateErrs or
// checkpointErrs queue errors for them.
type flakyAtelet struct {
	ateletpb.UnimplementedAteomHerderServer

	mu          sync.Mutex
	restoreErrs []error
	// terminateErrs fail the Terminate calls, one per call.
	terminateErrs []error
	failAlways    error
	// checkpointErrs fail the Checkpoint calls, one per call.
	checkpointErrs []error
	// onCheckpoint runs before each Checkpoint is answered, with its request.
	onCheckpoint func(*ateletpb.CheckpointRequest)
	// onRestore runs before each Restore is answered, with its number.
	onRestore   func(n int)
	restores    int
	terminates  int
	checkpoints int
}

func (f *flakyAtelet) Checkpoint(_ context.Context, req *ateletpb.CheckpointRequest) (*ateletpb.CheckpointResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkpoints++
	if f.onCheckpoint != nil {
		f.onCheckpoint(req)
	}
	if len(f.checkpointErrs) > 0 {
		err := f.checkpointErrs[0]
		f.checkpointErrs = f.checkpointErrs[1:]
		return nil, err
	}
	return &ateletpb.CheckpointResponse{}, nil
}

func (f *flakyAtelet) Restore(context.Context, *ateletpb.RestoreRequest) (*ateletpb.RestoreResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restores++
	if f.onRestore != nil {
		f.onRestore(f.restores)
	}
	if f.failAlways != nil {
		return nil, f.failAlways
	}
	if len(f.restoreErrs) > 0 {
		err := f.restoreErrs[0]
		f.restoreErrs = f.restoreErrs[1:]
		return nil, err
	}
	return &ateletpb.RestoreResponse{}, nil
}

func (f *flakyAtelet) Terminate(context.Context, *ateletpb.TerminateRequest) (*ateletpb.TerminateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminates++
	if len(f.terminateErrs) > 0 {
		err := f.terminateErrs[0]
		f.terminateErrs = f.terminateErrs[1:]
		return nil, err
	}
	return &ateletpb.TerminateResponse{}, nil
}

func (f *flakyAtelet) counts() (restores, terminates int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restores, f.terminates
}

// newResumeRetryFixture builds a whole Resume workflow over workers on
// node-1 (pod-1 and pod-2 unless pods names others), whose atelet is atelet,
// and a SUSPENDED actor team-a/id1 with a durable snapshot; opts mutate the
// stored actor's status.
func newResumeRetryFixture(t *testing.T, atelet *flakyAtelet, opts ...func(*ateapipb.ActorStatus)) (*ActorWorkflow, store.Interface, resources.ActorRef) {
	return newResumeRetryFixtureOn(t, []string{"pod-1", "pod-2"}, atelet, opts...)
}

func newResumeRetryFixtureOn(t *testing.T, pods []string, atelet *flakyAtelet, opts ...func(*ateapipb.ActorStatus)) (*ActorWorkflow, store.Interface, resources.ActorRef) {
	t.Helper()
	ctx := context.Background()
	st, cleanup := storetest.SetupTestStore(t)
	t.Cleanup(cleanup)
	w := newTestActorWorkflow(t, st, "ns", "tmpl1")
	storetest.MustCreateAtespace(t, ctx, st, "team-a")
	var fleet []*ateapipb.Worker
	for _, pod := range pods {
		fleet = append(fleet, activeWorker(pod, "gvisor", "node-1"))
	}
	wc := startedWorkerCache(t, ctx, st, fleet)
	w.workerCache, w.scheduler, w.nodeLister = wc, scheduling.New(wc), nodeListerOf(t, "node-1")
	w.dialer = newFakeAteletDialer(t, atelet)
	w.sandboxConfigLister = sandboxConfigListerFor(t, []*atev1alpha1.SandboxConfig{{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Spec: atev1alpha1.SandboxConfigSpec{
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			PauseImage:   "pause@sha256:abc",
			Assets:       testAssets(),
		},
	}})

	actorRef := resources.ActorRef{Atespace: "team-a", Name: "id1"}
	seedWorkflowActor(t, ctx, st, actorRef, "ns", "tmpl1", ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	tmpl, err := st.GetActorTemplate(ctx, resources.ActorTemplateRef{Atespace: "ns", Name: "tmpl1"})
	if err != nil {
		t.Fatalf("GetActorTemplate: %v", err)
	}
	snapshotURI := mustActorSnapshotURI(t, tmpl, actor, "snap-1").String()
	mustUpdateActorStatus(t, ctx, st, actor, func(s *ateapipb.ActorStatus) {
		s.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: snapshotURI, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL}
		for _, opt := range opts {
			opt(s)
		}
	})
	return w, st, actorRef
}

// TestResumeActor_TransientCrashRetries resumes an actor whose first restore
// meets a worker whose ateom is not listening yet: the actor goes back to its
// snapshot with its worker freed and the next attempt resumes it, so the
// caller sees one successful resume and no crash.
func TestResumeActor_TransientCrashRetries(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{restoreErrs: []error{ateomSocketGone}}
	w, st, actorRef := newResumeRetryFixture(t, atelet)
	before, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("ResumeActor = (state %v, resumed %v), want RUNNING and resumed", actor.GetStatus().GetState(), resumed)
	}
	if actor.GetStatus().GetCrash() != nil {
		t.Errorf("crash = %v, want none", actor.GetStatus().GetCrash())
	}
	if got, want := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(), before.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != want {
		t.Errorf("external snapshot = %q, want it intact at %q", got, want)
	}
	if restores, terminates := atelet.counts(); restores != 2 || terminates != 1 {
		t.Errorf("atelet calls = %d Restore, %d Terminate, want 2 and 1: one crashed restore torn down, then the resume", restores, terminates)
	}
	assertOneWorkerHosts(t, ctx, st, actor)
}

// TestResumeActor_TransientCrashWaitsForAFreeWorker resumes an actor whose
// only worker drains under its first restore: the actor goes back to its
// snapshot, the next resume finds no free worker and is retried rather than
// refused, and the resume succeeds once the worker is ready again.
func TestResumeActor_TransientCrashWaitsForAFreeWorker(t *testing.T) {
	ctx := context.Background()
	var st store.Interface
	setWorkerState := func(state ateapipb.WorkerState) {
		worker, err := st.GetWorker(ctx, testWorkerUID("pod-1"))
		if err != nil {
			t.Errorf("GetWorker: %v", err)
			return
		}
		if _, err := st.UpdateWorker(ctx, worker.GetMetadata().GetName(), store.PreconditionFrom(worker), func(toUpdate *ateapipb.Worker) error {
			toUpdate.Status.State = state
			return nil
		}); err != nil {
			t.Errorf("UpdateWorker(%v): %v", state, err)
		}
	}
	atelet := &flakyAtelet{restoreErrs: []error{ateomSocketGone}}
	atelet.onRestore = func(n int) {
		if n == 1 {
			// The worker drains under the first restore and is ready again
			// after the next resume found it busy.
			setWorkerState(ateapipb.WorkerState_WORKER_STATE_DRAINING)
			time.AfterFunc(1500*time.Millisecond, func() { setWorkerState(ateapipb.WorkerState_WORKER_STATE_ACTIVE) })
		}
	}
	w, fixtureStore, actorRef := newResumeRetryFixtureOn(t, []string{"pod-1"}, atelet)
	st = fixtureStore

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("ResumeActor = (state %v, resumed %v), want RUNNING and resumed", actor.GetStatus().GetState(), resumed)
	}
	if restores, _ := atelet.counts(); restores != 2 {
		t.Errorf("Restore calls = %d, want 2: the crashed one and the resume once the worker was back", restores)
	}
}

// TestResumeActor_TransientCrashUntilTheCallEnds keeps the ateom of every
// worker unreachable: the call answers Unavailable when its context ends, and
// the actor is left SUSPENDED on its snapshot, holding no worker, for the
// caller's retry.
func TestResumeActor_TransientCrashUntilTheCallEnds(t *testing.T) {
	atelet := &flakyAtelet{failAlways: ateomSocketGone}
	w, st, actorRef := newResumeRetryFixture(t, atelet)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	_, _, err := w.ResumeActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ResumeActor error = %v, want Unavailable", err)
	}
	stored, err := st.GetActor(context.Background(), actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("state = %v, want SUSPENDED", got)
	}
	if stored.GetStatus().GetCrash() != nil || stored.GetStatus().GetWorkerAssignment() != nil {
		t.Errorf("status = %v, want no crash and no worker", stored.GetStatus())
	}
	if restores, _ := atelet.counts(); restores != 2 {
		t.Errorf("Restore calls = %d, want 2 within the call's context", restores)
	}
	if _, err := st.FindWorkerHostingActor(context.Background(), stored.GetMetadata().GetUid()); err == nil {
		t.Error("a worker still hosts the actor, want it freed")
	}
}

// TestResumeActor_WorkerGoneUnderRestore resumes an actor whose worker pod
// goes away mid-restore: the sandbox is killed and the crash's teardown cannot
// reach the worker. The crash records the unreachable worker, the actor is
// recovered once the worker is torn down, and the retry resumes it.
func TestResumeActor_WorkerGoneUnderRestore(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{restoreErrs: []error{runscKilled}, terminateErrs: []error{ateomGone}}
	w, _, actorRef := newResumeRetryFixture(t, atelet)

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING || actor.GetStatus().GetCrash() != nil {
		t.Errorf("ResumeActor = (status %v, resumed %v), want RUNNING, resumed and no crash", actor.GetStatus(), resumed)
	}
	if restores, terminates := atelet.counts(); restores != 2 || terminates != 2 {
		t.Errorf("atelet calls = %d Restore, %d Terminate, want 2 and 2: the crash's teardown failed, the recovery's tore it down", restores, terminates)
	}
}

// TestResumeActor_RestoreCutByWorkerLeaving resumes an actor whose worker pod
// is deleted mid-restore while atelet stays reachable: the sandbox is killed,
// the crash's teardown succeeds, and the worker is draining or already gone.
// The crash records the worker's state, the actor goes back to its snapshot
// and the retry resumes it on the other worker.
func TestResumeActor_RestoreCutByWorkerLeaving(t *testing.T) {
	tests := []struct {
		name  string
		leave func(t *testing.T, st store.Interface, worker *ateapipb.Worker)
		// terminates is 1 either way: the teardown terminates the actor's
		// workload on its assigned node, which outlives the worker's record.
		terminates int
	}{
		{"draining", func(t *testing.T, st store.Interface, worker *ateapipb.Worker) {
			if _, err := st.UpdateWorker(context.Background(), worker.GetMetadata().GetName(), store.PreconditionFrom(worker), func(toUpdate *ateapipb.Worker) error {
				toUpdate.Status.State = ateapipb.WorkerState_WORKER_STATE_DRAINING
				return nil
			}); err != nil {
				t.Errorf("UpdateWorker: %v", err)
			}
		}, 1},
		{"gone", func(t *testing.T, st store.Interface, worker *ateapipb.Worker) {
			if _, err := st.DeleteWorker(context.Background(), worker.GetMetadata().GetName(), store.DeletePreconditions{}); err != nil {
				t.Errorf("DeleteWorker: %v", err)
			}
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var st store.Interface
			var actorRef resources.ActorRef
			var cutWorker string
			atelet := &flakyAtelet{restoreErrs: []error{runscKilled}}
			atelet.onRestore = func(n int) {
				if n != 1 {
					return
				}
				actor, err := st.GetActor(ctx, actorRef)
				if err != nil {
					t.Errorf("GetActor: %v", err)
					return
				}
				name, err := st.FindWorkerHostingActor(ctx, actor.GetMetadata().GetUid())
				if err != nil {
					t.Errorf("FindWorkerHostingActor: %v", err)
					return
				}
				worker, err := st.GetWorker(ctx, name)
				if err != nil {
					t.Errorf("GetWorker: %v", err)
					return
				}
				cutWorker = name
				tt.leave(t, st, worker)
			}
			w, fixtureStore, ref := newResumeRetryFixture(t, atelet)
			st, actorRef = fixtureStore, ref

			actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
			if err != nil {
				t.Fatalf("ResumeActor: %v", err)
			}
			if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING || actor.GetStatus().GetCrash() != nil {
				t.Errorf("ResumeActor = (status %v, resumed %v), want RUNNING, resumed and no crash", actor.GetStatus(), resumed)
			}
			if got := actor.GetStatus().GetWorkerAssignment().GetWorker().GetName(); got == cutWorker {
				t.Errorf("actor runs on worker %q, want the other worker", got)
			}
			if restores, terminates := atelet.counts(); restores != 2 || terminates != tt.terminates {
				t.Errorf("atelet calls = %d Restore, %d Terminate, want 2 and %d", restores, terminates, tt.terminates)
			}
		})
	}
}

// TestResumeActor_RecoveryCutUntilTheCallEnds resumes an actor whose worker
// pod goes away mid-restore and whose ateom stays unreachable, so neither the
// crash's teardown nor the recovery's reaches it before the call ends. The call
// answers Unavailable naming the crash, never the restore's Internal, and the
// actor returns to rest once its worker can be torn down.
func TestResumeActor_RecoveryCutUntilTheCallEnds(t *testing.T) {
	gone := make([]error, 20)
	for i := range gone {
		gone[i] = ateomGone
	}
	atelet := &flakyAtelet{restoreErrs: []error{runscKilled}, terminateErrs: gone}
	w, st, actorRef := newResumeRetryFixture(t, atelet)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	_, _, err := w.ResumeActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ResumeActor error = %v, want Unavailable", err)
	}
	if msg := status.Convert(err).Message(); !strings.Contains(msg, crashMessageWorkerUnreachable) {
		t.Errorf("ResumeActor error message = %q, want the crash's cause named", msg)
	}

	atelet.mu.Lock()
	atelet.terminateErrs = nil
	atelet.mu.Unlock()
	if _, recovered, err := w.recoverTransientCrash(context.Background(), actorRef); err != nil || !recovered {
		t.Fatalf("recoverTransientCrash = (%v, %v), want the actor recovered", recovered, err)
	}
	stored, err := st.GetActor(context.Background(), actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || stored.GetStatus().GetCrash() != nil {
		t.Errorf("status = %v, want SUSPENDED and no crash", stored.GetStatus())
	}
}

// TestResumeActor_TransientCrashAfterTheCallEnded answers Unavailable for an
// actor a resume crashed for a transient cause when the call's context has
// already ended: the crash is read regardless and the actor returned to rest.
func TestResumeActor_TransientCrashAfterTheCallEnded(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{}
	w, st, actorRef := newResumeRetryFixture(t, atelet, func(s *ateapipb.ActorStatus) {
		s.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
		s.Crash = &ateapipb.ActorCrash{Message: wakeupProbeCancelledCrash}
	})
	ended, cancel := context.WithCancel(ctx)
	cancel()

	_, _, err := w.ResumeActor(ended, actorRef, nil)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ResumeActor error = %v, want Unavailable", err)
	}
	if msg := status.Convert(err).Message(); !strings.Contains(msg, "wakeup probe cancelled") {
		t.Errorf("ResumeActor error message = %q, want the crash's cause named", msg)
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || stored.GetStatus().GetCrash() != nil {
		t.Errorf("status = %v, want SUSPENDED and no crash", stored.GetStatus())
	}
}

// TestResumeActor_SandboxKilledOnALiveWorkerStaysCrashed keeps the crash of a
// sandbox killed on a worker that is still there, as the kernel kills one for
// its memory: its teardown reaches the worker, so no retry outlives it.
func TestResumeActor_SandboxKilledOnALiveWorkerStaysCrashed(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{restoreErrs: []error{runscKilled}}
	w, st, actorRef := newResumeRetryFixture(t, atelet)

	_, _, err := w.ResumeActor(ctx, actorRef, nil)
	if got := apierror.Code(err); got != codes.Internal {
		t.Fatalf("ResumeActor error = %v, want the crash's Internal", err)
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Errorf("state = %v, want CRASHED", got)
	}
	if msg := stored.GetStatus().GetCrash().GetMessage(); strings.Contains(msg, crashMessageWorkerUnreachable) {
		t.Errorf("crash = %q, want no unreachable worker named", msg)
	}
}

// TestResumeActor_NonTransientCrashStaysCrashed keeps the terminal crash for a
// restore failure no retry outlives.
func TestResumeActor_NonTransientCrashStaysCrashed(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{restoreErrs: []error{status.Error(codes.Unknown, "while creating workload from spec: MANIFEST_UNKNOWN: manifest unknown")}}
	w, st, actorRef := newResumeRetryFixture(t, atelet)

	if _, _, err := w.ResumeActor(ctx, actorRef, nil); err == nil {
		t.Fatal("ResumeActor returned no error, want the crash")
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Errorf("state = %v, want CRASHED", got)
	}
	if restores, _ := atelet.counts(); restores != 1 {
		t.Errorf("Restore calls = %d, want 1", restores)
	}
}

func TestOutlivedResumeCrash(t *testing.T) {
	crashed := ateapipb.ActorState_ACTOR_STATE_CRASHED
	named := &ateapipb.ActorTemplate{}
	withTrustBundle("egress-mitm.ate.dev")(named)
	nameless := &ateapipb.ActorTemplate{}
	withTrustBundle()(nameless)
	tests := []struct {
		name  string
		state ateapipb.ActorState
		crash string
		tmpl  *ateapipb.ActorTemplate
		want  bool
	}{
		{"template names its bundle now", crashed, trustBundleNamesCrash, named, true},
		{"template without trust bundles", crashed, trustBundleNamesCrash, &ateapipb.ActorTemplate{}, true},
		{"template still names none", crashed, trustBundleNamesCrash, nameless, false},
		{"template gone", crashed, trustBundleNamesCrash, nil, false},
		// The actor ran after its snapshot: going back to it would lose work.
		{"suspend refused", crashed, strings.Replace(trustBundleNamesCrash, "resume failed: ", "suspend failed: ", 1), named, false},
		{"another resume crash", crashed, "resume failed: atelet Run: no runnable process in container", named, false},
		{"recovered since", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, trustBundleNamesCrash, named, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: tt.state, Crash: &ateapipb.ActorCrash{Message: tt.crash}}}
			if got := outlivedResumeCrash(actor, tt.tmpl); got != tt.want {
				t.Errorf("outlivedResumeCrash(%v, %q) = %v, want %v", tt.state, tt.crash, got, tt.want)
			}
		})
	}
}

// trustBundleGuardCrashed records the crash a resume met on atelet's
// trust-bundle guard, under a release that read the actor's template with no
// names, on the actor resting at its snapshot.
func trustBundleGuardCrashed(s *ateapipb.ActorStatus) {
	s.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	s.Crash = &ateapipb.ActorCrash{Message: trustBundleNamesCrash}
}

// TestResumeActor_TrustBundleGuardCrashResumes resumes a conversation actor a
// resume left CRASHED on atelet's trust-bundle guard, its template row
// rewritten without the trust-bundle name: the store reads the projection with
// its one name, so the actor returns to the snapshot it was resuming from and
// the resume places it on a worker, with no operator action.
func TestResumeActor_TrustBundleGuardCrashResumes(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{}
	w, st, actorRef := newResumeRetryFixture(t, atelet, trustBundleGuardCrashed)
	templateRef := resources.ActorTemplateRef{Atespace: "ns", Name: "tmpl1"}
	tmpl, err := st.GetActorTemplate(ctx, templateRef)
	if err != nil {
		t.Fatalf("GetActorTemplate: %v", err)
	}
	if _, err := st.UpdateActorTemplate(ctx, templateRef, store.PreconditionFrom(tmpl), func(db *ateapipb.ActorTemplate) error {
		withTrustBundle()(db)
		return nil
	}); err != nil {
		t.Fatalf("writing the rewritten template row: %v", err)
	}
	before, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}

	actor, resumed, err := w.ResumeActor(ctx, actorRef, nil)
	if err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	if !resumed || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("ResumeActor = (state %v, resumed %v), want RUNNING and resumed", actor.GetStatus().GetState(), resumed)
	}
	if actor.GetStatus().GetCrash() != nil {
		t.Errorf("crash = %v, want it cleared", actor.GetStatus().GetCrash())
	}
	if got, want := actor.GetStatus().GetExternalSnapshot().GetSnapshotUri(), before.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got == "" || got != want {
		t.Errorf("external snapshot = %q, want it intact at %q", got, want)
	}
	if restores, _ := atelet.counts(); restores != 1 {
		t.Errorf("Restore calls = %d, want 1: the resume from the snapshot", restores)
	}
	assertOneWorkerHosts(t, ctx, st, actor)
}

// namelessTemplateStore reads every ActorTemplate with a trust-bundle
// projection that names no bundle, as a store without the read of a rewritten
// row does.
type namelessTemplateStore struct {
	store.Interface
}

func (s namelessTemplateStore) GetActorTemplate(ctx context.Context, ref resources.ActorTemplateRef) (*ateapipb.ActorTemplate, error) {
	tmpl, err := s.Interface.GetActorTemplate(ctx, ref)
	if err != nil {
		return nil, err
	}
	withTrustBundle()(tmpl)
	return tmpl, nil
}

// TestResumeActor_TrustBundleGuardCrashStaysWhileTemplateNamesNone keeps an
// actor the trust-bundle guard crashed CRASHED while its template still names
// no bundle: nothing resumes it into the same refusal.
func TestResumeActor_TrustBundleGuardCrashStaysWhileTemplateNamesNone(t *testing.T) {
	ctx := context.Background()
	atelet := &flakyAtelet{}
	w, st, actorRef := newResumeRetryFixture(t, atelet, trustBundleGuardCrashed)
	w.store = namelessTemplateStore{st}

	if _, _, err := w.ResumeActor(ctx, actorRef, nil); err == nil {
		t.Fatal("ResumeActor returned no error, want the actor's crash refused")
	}
	stored, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	if got := stored.GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		t.Errorf("state = %v, want CRASHED", got)
	}
	if got := stored.GetStatus().GetCrash().GetMessage(); got != trustBundleNamesCrash {
		t.Errorf("crash = %q, want %q kept", got, trustBundleNamesCrash)
	}
	if restores, _ := atelet.counts(); restores != 0 {
		t.Errorf("Restore calls = %d, want none", restores)
	}
}

// TestRecoverTransientCrash returns a transiently crashed actor to the
// state its resume started from, keeping a pause snapshot the resume was
// restoring, returns one a checkpoint or a lost worker crashed to its own
// external snapshot, dropping its pause snapshot, and leaves any other actor,
// and one whose external snapshot is its template's golden, alone.
func TestRecoverTransientCrash(t *testing.T) {
	tests := []struct {
		name          string
		crash         string
		golden        bool
		local         *ateapipb.LocalSnapshot
		dropsLocal    bool
		wantRecovered bool
		wantState     ateapipb.ActorState
	}{
		{name: "suspended actor", crash: "resume failed: " + crashMessageWorkerDraining, wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "paused actor keeps its pause", crash: "resume failed: " + crashMessageWorkerPodGone, local: &ateapipb.LocalSnapshot{SnapshotName: "local-1", NodeName: "node-1"},
			wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_PAUSED},
		{name: "another cause stays crashed", crash: "resume failed: atelet Run: no runnable process in container", wantState: ateapipb.ActorState_ACTOR_STATE_CRASHED},
		{name: "checkpoint returns to the external snapshot", crash: "suspend failed: " + crashMessageWorkerPodGone, local: &ateapipb.LocalSnapshot{SnapshotName: "local-1", NodeName: "node-1"},
			dropsLocal: true, wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "a lost worker returns to the external snapshot", crash: crashMessageWorkerPodGone, local: &ateapipb.LocalSnapshot{SnapshotName: "local-1", NodeName: "node-1"},
			dropsLocal: true, wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		// The golden carries none of what the actor did since it booted.
		{name: "a lost worker on the golden stays crashed", crash: crashMessageWorkerPodGone, golden: true, wantState: ateapipb.ActorState_ACTOR_STATE_CRASHED},
		{name: "a checkpoint on the golden stays crashed", crash: "suspend failed: " + crashMessageWorkerPodGone, golden: true, wantState: ateapipb.ActorState_ACTOR_STATE_CRASHED},
		{name: "trust-bundle guard returns to the snapshot", crash: trustBundleNamesCrash, wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "trust-bundle guard keeps the pause", crash: trustBundleNamesCrash, local: &ateapipb.LocalSnapshot{SnapshotName: "local-1", NodeName: "node-1"},
			wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_PAUSED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			w := newTestActorWorkflow(t, st, "ns", "tmpl1")
			actorRef := resources.ActorRef{Atespace: "team-a", Name: "id1"}
			storetest.MustCreateAtespace(t, ctx, st, "team-a")
			seedWorkflowActor(t, ctx, st, actorRef, "ns", "tmpl1", ateapipb.ActorState_ACTOR_STATE_CRASHED, func(a *ateapipb.Actor) {
				a.Status.Crash = &ateapipb.ActorCrash{Message: tt.crash}
				a.Status.LocalSnapshot = tt.local
			})
			keptURI := seedExternalSnapshot(t, ctx, st, actorRef, tt.golden)

			crash, recovered, err := w.recoverTransientCrash(ctx, actorRef)
			if err != nil {
				t.Fatalf("recoverTransientCrash: %v", err)
			}
			if recovered != tt.wantRecovered || (recovered && crash != tt.crash) {
				t.Errorf("recoverTransientCrash = (%q, %v), want (%q, %v)", crash, recovered, tt.crash, tt.wantRecovered)
			}
			stored, err := st.GetActor(ctx, actorRef)
			if err != nil {
				t.Fatalf("GetActor: %v", err)
			}
			if got := stored.GetStatus().GetState(); got != tt.wantState {
				t.Errorf("state = %v, want %v", got, tt.wantState)
			}
			if got := stored.GetStatus().GetExternalSnapshot().GetSnapshotUri(); got != keptURI {
				t.Errorf("external snapshot = %q, want it intact at %q", got, keptURI)
			}
			wantLocal := tt.local.GetSnapshotName()
			if tt.dropsLocal {
				wantLocal = ""
			}
			if got := stored.GetStatus().GetLocalSnapshot().GetSnapshotName(); got != wantLocal {
				t.Errorf("local snapshot = %q, want %q", got, wantLocal)
			}
			if tt.wantRecovered && stored.GetStatus().GetCrash() != nil {
				t.Errorf("crash = %v, want it cleared", stored.GetStatus().GetCrash())
			}
			if !tt.wantRecovered && stored.GetStatus().GetCrash().GetMessage() != tt.crash {
				t.Errorf("crash = %v, want %q kept", stored.GetStatus().GetCrash(), tt.crash)
			}
		})
	}
}

// seedExternalSnapshot records an external snapshot on the stored actor and
// returns its URI: one the actor wrote, or its template's golden.
func seedExternalSnapshot(t *testing.T, ctx context.Context, st store.Interface, actorRef resources.ActorRef, golden bool) string {
	t.Helper()
	actor, err := st.GetActor(ctx, actorRef)
	if err != nil {
		t.Fatalf("GetActor: %v", err)
	}
	tmpl, err := st.GetActorTemplate(ctx, resources.ActorTemplateRef{Atespace: "ns", Name: "tmpl1"})
	if err != nil {
		t.Fatalf("GetActorTemplate: %v", err)
	}
	uri := mustActorSnapshotURI(t, tmpl, actor, "keep").String()
	if golden {
		uri = mustTagSnapshotURI(t, tmpl, resources.GoldenActorAtespace, "golden-1").String()
	}
	mustUpdateActorStatus(t, ctx, st, actor, func(s *ateapipb.ActorStatus) {
		s.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri}
	})
	return uri
}

// assertOneWorkerHosts checks that exactly the worker the actor names hosts
// it: the crashed attempt's worker was freed.
func assertOneWorkerHosts(t *testing.T, ctx context.Context, st store.Interface, actor *ateapipb.Actor) {
	t.Helper()
	workerName, err := st.FindWorkerHostingActor(ctx, actor.GetMetadata().GetUid())
	if err != nil {
		t.Fatalf("FindWorkerHostingActor: %v", err)
	}
	if want := actor.GetStatus().GetWorkerAssignment().GetWorker().GetName(); workerName != want {
		t.Errorf("worker hosting the actor = %q, want %q, the one it names", workerName, want)
	}
}
