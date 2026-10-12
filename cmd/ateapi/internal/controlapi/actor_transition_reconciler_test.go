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
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeTransitionStore serves a fixed set of Actors, one per page.
type fakeTransitionStore struct {
	actors []*ateapipb.Actor
}

func (f *fakeTransitionStore) GetActor(_ context.Context, ref resources.ActorRef) (*ateapipb.Actor, error) {
	for _, a := range f.actors {
		if resources.ActorRefFromActor(a) == ref {
			return a, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeTransitionStore) ListActors(_ context.Context, atespace string, opts store.ListOptions) (store.ListResponse[*ateapipb.Actor], error) {
	if atespace != "" {
		return store.ListResponse[*ateapipb.Actor]{}, errors.New("want a list across all atespaces")
	}
	i := 0
	if opts.PageToken != "" {
		i = slices.IndexFunc(f.actors, func(a *ateapipb.Actor) bool { return a.GetMetadata().GetName() == opts.PageToken })
	}
	if i >= len(f.actors) {
		return store.ListResponse[*ateapipb.Actor]{}, nil
	}
	resp := store.ListResponse[*ateapipb.Actor]{Items: f.actors[i : i+1]}
	if i+1 < len(f.actors) {
		resp.NextPageToken = f.actors[i+1].GetMetadata().GetName()
	}
	return resp, nil
}

// fakeTransitionControl records the workflows the reconciler re-enters.
type fakeTransitionControl struct {
	suspended []string
	paused    []string
	recovered []string
	err       error
}

func (f *fakeTransitionControl) SuspendActor(_ context.Context, req *ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error) {
	f.suspended = append(f.suspended, req.GetActor().GetName())
	return &ateapipb.SuspendActorResponse{}, f.err
}

func (f *fakeTransitionControl) PauseActor(_ context.Context, req *ateapipb.PauseActorRequest) (*ateapipb.PauseActorResponse, error) {
	f.paused = append(f.paused, req.GetActor().GetName())
	return &ateapipb.PauseActorResponse{}, f.err
}

func (f *fakeTransitionControl) RecoverTransientResumeCrash(_ context.Context, ref resources.ActorRef) (bool, error) {
	f.recovered = append(f.recovered, ref.Name)
	return f.err == nil, f.err
}

const transitionDeadline = 5 * time.Minute

var transitionNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func transitionActor(name string, state ateapipb.ActorState, age time.Duration) *ateapipb.Actor {
	return &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: name, UpdateTime: timestamppb.New(transitionNow.Add(-age))},
		Status:   &ateapipb.ActorStatus{State: state},
	}
}

// crashedActor is an Actor CRASHED with message, as long ago as age.
func crashedActor(name, message string, age time.Duration) *ateapipb.Actor {
	actor := transitionActor(name, ateapipb.ActorState_ACTOR_STATE_CRASHED, age)
	actor.Status.Crash = &ateapipb.ActorCrash{Message: message}
	return actor
}

func newTestTransitionReconciler(st actorTransitionStore, control actorTransitionControl) *ActorTransitionReconciler {
	r := NewActorTransitionReconciler(st, control, time.Hour, transitionDeadline)
	r.now = func() time.Time { return transitionNow }
	return r
}

// TestActorTransitionReconciler_QueuesOrphanedTransitions verifies that the
// resync queues exactly the Actors left SUSPENDING or PAUSING for longer than
// the workflow deadline, across every page: a fresher one may still be owned
// by a live request.
func TestActorTransitionReconciler_QueuesOrphanedTransitions(t *testing.T) {
	st := &fakeTransitionStore{actors: []*ateapipb.Actor{
		transitionActor("suspending-orphaned", ateapipb.ActorState_ACTOR_STATE_SUSPENDING, transitionDeadline+time.Second),
		transitionActor("pausing-orphaned", ateapipb.ActorState_ACTOR_STATE_PAUSING, time.Hour),
		transitionActor("suspending-in-flight", ateapipb.ActorState_ACTOR_STATE_SUSPENDING, transitionDeadline-time.Second),
		transitionActor("running", ateapipb.ActorState_ACTOR_STATE_RUNNING, time.Hour),
		transitionActor("resuming", ateapipb.ActorState_ACTOR_STATE_RESUMING, time.Hour),
		transitionActor("suspended", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, time.Hour),
		// A resume crash a rolling worker pool caused is queued at once, a
		// crash for another cause or by another operation never.
		crashedActor("resume-crashed-draining", "resume failed: "+crashMessageWorkerDraining, time.Second),
		crashedActor("resume-crashed-manifest", "resume failed: atelet Restore: MANIFEST_UNKNOWN: manifest unknown", time.Hour),
		crashedActor("suspend-crashed-pod-gone", "suspend failed: "+crashMessageWorkerPodGone, time.Hour),
	}}
	r := newTestTransitionReconciler(st, &fakeTransitionControl{})
	defer r.queue.ShutDown()

	r.resync(context.Background())

	var got []string
	for r.queue.Len() > 0 {
		ref, _ := r.queue.Get()
		got = append(got, ref.Name)
		r.queue.Done(ref)
	}
	slices.Sort(got)
	want := []string{"pausing-orphaned", "resume-crashed-draining", "suspending-orphaned"}
	if !slices.Equal(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
}

// TestActorTransitionReconciler_ReentersTheStatesWorkflow verifies that an
// orphaned Actor is driven by the workflow its state names, and that one a
// request took up since the list is left alone.
func TestActorTransitionReconciler_ReentersTheStatesWorkflow(t *testing.T) {
	tests := []struct {
		name          string
		actor         *ateapipb.Actor
		wantSuspended []string
		wantPaused    []string
		wantRecovered []string
	}{
		{
			name:          "suspending",
			actor:         transitionActor("a", ateapipb.ActorState_ACTOR_STATE_SUSPENDING, time.Hour),
			wantSuspended: []string{"a"},
		},
		{
			name:       "pausing",
			actor:      transitionActor("a", ateapipb.ActorState_ACTOR_STATE_PAUSING, time.Hour),
			wantPaused: []string{"a"},
		},
		{
			name:  "touched since the list",
			actor: transitionActor("a", ateapipb.ActorState_ACTOR_STATE_SUSPENDING, time.Second),
		},
		{
			name:  "finished since the list",
			actor: transitionActor("a", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, time.Hour),
		},
		{
			name:          "crashed by a resume a rolling worker pool interrupted",
			actor:         crashedActor("a", "resume failed: atelet Restore: transport: Error while dialing: dial unix /run/ate/workers/w-1/ateom.sock: connect: no such file or directory", time.Second),
			wantRecovered: []string{"a"},
		},
		{
			name:  "crashed by a resume for another cause",
			actor: crashedActor("a", "resume failed: atelet Run: no runnable process in container", time.Hour),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := &fakeTransitionControl{}
			r := newTestTransitionReconciler(&fakeTransitionStore{actors: []*ateapipb.Actor{tt.actor}}, control)
			defer r.queue.ShutDown()

			if err := r.reconcileOne(context.Background(), resources.ActorRefFromActor(tt.actor)); err != nil {
				t.Fatalf("reconcileOne: %v", err)
			}
			if !slices.Equal(control.suspended, tt.wantSuspended) {
				t.Errorf("SuspendActor called for %v, want %v", control.suspended, tt.wantSuspended)
			}
			if !slices.Equal(control.paused, tt.wantPaused) {
				t.Errorf("PauseActor called for %v, want %v", control.paused, tt.wantPaused)
			}
			if !slices.Equal(control.recovered, tt.wantRecovered) {
				t.Errorf("RecoverTransientResumeCrash called for %v, want %v", control.recovered, tt.wantRecovered)
			}
		})
	}
}

// TestActorTransitionReconciler_WorkflowErrors verifies that a lease held by a
// client's retry is left to that client, and any other failure is returned for
// the queue to retry.
func TestActorTransitionReconciler_WorkflowErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "lease held elsewhere", err: status.Error(codes.Aborted, "another operation is in progress for this actor")},
		{name: "atelet unreachable", err: status.Error(codes.Unavailable, "connection refused"), wantErr: true},
		{name: "missing actor", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := transitionActor("a", ateapipb.ActorState_ACTOR_STATE_SUSPENDING, time.Hour)
			st := &fakeTransitionStore{actors: []*ateapipb.Actor{actor}}
			ref := resources.ActorRefFromActor(actor)
			if tt.err == nil {
				st.actors = nil
			}
			r := newTestTransitionReconciler(st, &fakeTransitionControl{err: tt.err})
			defer r.queue.ShutDown()

			err := r.reconcileOne(context.Background(), ref)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Errorf("reconcileOne = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}
