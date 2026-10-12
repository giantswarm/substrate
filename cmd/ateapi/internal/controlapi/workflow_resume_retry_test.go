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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/scheduling"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
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
		{"manifest unknown", crashed, "resume failed: atelet Restore: MANIFEST_UNKNOWN: manifest unknown", false},
		{"no runnable process", crashed, "resume failed: atelet Run: no runnable process in container", false},
		{"wakeup probe never answered", crashed, `resume failed: atelet Run: while calling ateom.RunWorkload: rpc error: code = Unknown desc = while waiting for container wakeup probe: wakeup probe for "kagent" never returned 200 within 2m0s (40 attempts, last error: connection refused)`, false},
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
// Terminate with success.
type flakyAtelet struct {
	ateletpb.UnimplementedAteomHerderServer

	mu          sync.Mutex
	restoreErrs []error
	failAlways  error
	restores    int
	terminates  int
}

func (f *flakyAtelet) Restore(context.Context, *ateletpb.RestoreRequest) (*ateletpb.RestoreResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restores++
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
	return &ateletpb.TerminateResponse{}, nil
}

func (f *flakyAtelet) counts() (restores, terminates int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restores, f.terminates
}

// newResumeRetryFixture builds a whole Resume workflow over two workers on
// node-1, whose atelet is atelet, and a SUSPENDED actor team-a/id1 with a
// durable snapshot; opts mutate the stored actor's status.
func newResumeRetryFixture(t *testing.T, atelet *flakyAtelet, opts ...func(*ateapipb.ActorStatus)) (*ActorWorkflow, store.Interface, resources.ActorRef) {
	t.Helper()
	ctx := context.Background()
	st, cleanup := storetest.SetupTestStore(t)
	t.Cleanup(cleanup)
	w := newTestActorWorkflow(t, st, "ns", "tmpl1")
	storetest.MustCreateAtespace(t, ctx, st, "team-a")
	wc := startedWorkerCache(t, ctx, st, []*ateapipb.Worker{
		activeWorker("pod-1", "gvisor", "node-1"),
		activeWorker("pod-2", "gvisor", "node-1"),
	})
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
	if got := status.Code(err); got != codes.Unavailable {
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

// TestRecoverTransientResumeCrash returns a transiently crashed actor to the
// state its resume started from, keeping a pause snapshot the resume was
// restoring, and leaves any other actor alone.
func TestRecoverTransientResumeCrash(t *testing.T) {
	const keptURI = "gs://snapshots/team-a/actors/keep/snapshot"
	tests := []struct {
		name          string
		crash         string
		local         *ateapipb.LocalSnapshot
		wantRecovered bool
		wantState     ateapipb.ActorState
	}{
		{name: "suspended actor", crash: "resume failed: " + crashMessageWorkerDraining, wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "paused actor keeps its pause", crash: "resume failed: " + crashMessageWorkerPodGone, local: &ateapipb.LocalSnapshot{SnapshotName: "local-1", NodeVmsWithLocalSnapshots: []string{"node-1"}},
			wantRecovered: true, wantState: ateapipb.ActorState_ACTOR_STATE_PAUSED},
		{name: "another cause stays crashed", crash: "resume failed: atelet Run: no runnable process in container", wantState: ateapipb.ActorState_ACTOR_STATE_CRASHED},
		{name: "another operation stays crashed", crash: "suspend failed: " + crashMessageWorkerPodGone, wantState: ateapipb.ActorState_ACTOR_STATE_CRASHED},
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
				a.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: keptURI}
				a.Status.LocalSnapshot = tt.local
			})

			crash, recovered, err := w.recoverTransientResumeCrash(ctx, actorRef)
			if err != nil {
				t.Fatalf("recoverTransientResumeCrash: %v", err)
			}
			if recovered != tt.wantRecovered || (recovered && crash != tt.crash) {
				t.Errorf("recoverTransientResumeCrash = (%q, %v), want (%q, %v)", crash, recovered, tt.crash, tt.wantRecovered)
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
			if got := stored.GetStatus().GetLocalSnapshot().GetSnapshotName(); got != tt.local.GetSnapshotName() {
				t.Errorf("local snapshot = %q, want %q kept", got, tt.local.GetSnapshotName())
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
