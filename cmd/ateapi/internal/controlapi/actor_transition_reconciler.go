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
	"fmt"
	"log/slog"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

const (
	actorTransitionListPageSize = 100

	// actorTransitionWorkerCount is the number of goroutines draining the work
	// queue.
	actorTransitionWorkerCount = 2
)

// actorTransitionStore enumerates the exact storage methods needed by
// ActorTransitionReconciler and nothing more.
type actorTransitionStore interface {
	GetActor(ctx context.Context, actorRef resources.ActorRef) (*ateapipb.Actor, error)
	ListActors(ctx context.Context, atespace string, opts store.ListOptions) (store.ListResponse[*ateapipb.Actor], error)
}

// actorTransitionControl is the in-process slice of the Control service the
// reconciler re-enters the lifecycle workflows through. *RPCService satisfies
// it.
type actorTransitionControl interface {
	SuspendActor(ctx context.Context, req *ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error)
	PauseActor(ctx context.Context, req *ateapipb.PauseActorRequest) (*ateapipb.PauseActorResponse, error)
}

// ActorTransitionReconciler finishes the suspends and pauses whose request was
// lost. The state SUSPENDING or PAUSING is committed before the atelet call, so
// an ate-api replica that dies mid-request leaves the Actor in it with nothing
// driving it on: its caller only sees an error and may never retry.
//
// An Actor is orphaned once it has sat in a transition, untouched, for longer
// than the workflow deadline: every workflow commits that state under the
// actor lease, and the lease bounds the workflow to the deadline, so no live
// request can still own it. The reconciler re-enters the same workflow, which
// is reentrant and fast-forwards past the steps already done: the Actor ends
// SUSPENDED or PAUSED once the atelet answers, or CRASHED with the reason when
// the atelet reports the workload gone. The actor lease keeps the re-entry from
// racing a client that retries at the same time.
//
// TODO: Every ateapi replica lists every Actor, with only the actor lease
// keeping them from driving the same one at once, as the other reconcilers do.
type ActorTransitionReconciler struct {
	persistence    actorTransitionStore
	control        actorTransitionControl
	queue          workqueue.TypedRateLimitingInterface[resources.ActorRef]
	resyncInterval time.Duration
	orphanAfter    time.Duration
	now            func() time.Time
}

// NewActorTransitionReconciler creates the reconciler. orphanAfter is the
// actor workflow deadline: how long an Actor must sit in a transition, unchanged,
// before no request can still own it.
func NewActorTransitionReconciler(persistence actorTransitionStore, control actorTransitionControl, resyncInterval, orphanAfter time.Duration) *ActorTransitionReconciler {
	return &ActorTransitionReconciler{
		persistence:    persistence,
		control:        control,
		resyncInterval: resyncInterval,
		orphanAfter:    orphanAfter,
		now:            time.Now,
		queue:          workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[resources.ActorRef]()),
	}
}

// Start launches the queue workers and the resync producer; a lost request
// emits no event, so the periodic list is the event source.
func (r *ActorTransitionReconciler) Start(ctx context.Context) {
	go func() {
		defer r.queue.ShutDown()
		for range actorTransitionWorkerCount {
			go wait.UntilWithContext(ctx, r.runWorker, time.Second)
		}
		wait.UntilWithContext(ctx, r.resync, r.resyncInterval)
	}()
}

// resync lists every Actor and queues the orphaned transitions.
func (r *ActorTransitionReconciler) resync(ctx context.Context) {
	pageToken := ""
	for {
		page, err := r.persistence.ListActors(ctx, "", store.ListOptions{PageSize: actorTransitionListPageSize, PageToken: pageToken})
		if err != nil {
			slog.ErrorContext(ctx, "Failed to list actors", slog.Any("err", err))
			return
		}
		for _, actor := range page.Items {
			if r.orphaned(actor) {
				r.queue.Add(resources.ActorRefFromActor(actor))
			}
		}
		if page.NextPageToken == "" {
			return
		}
		pageToken = page.NextPageToken
	}
}

// orphaned reports whether actor sits in a transition no request can still
// own.
func (r *ActorTransitionReconciler) orphaned(actor *ateapipb.Actor) bool {
	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDING, ateapipb.ActorState_ACTOR_STATE_PAUSING:
	default:
		return false
	}
	updated := actor.GetMetadata().GetUpdateTime()
	return updated == nil || r.now().Sub(updated.AsTime()) > r.orphanAfter
}

func (r *ActorTransitionReconciler) runWorker(ctx context.Context) {
	for r.processNextWorkItem(ctx) {
	}
}

func (r *ActorTransitionReconciler) processNextWorkItem(ctx context.Context) bool {
	ref, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(ref)

	if err := r.reconcileOne(ctx, ref); err != nil {
		slog.LogAttrs(ctx, slog.LevelError, "Failed to finish an orphaned actor transition, requeueing",
			append(ateattr.ActorRefLogAttrs(ref), slog.Any("err", err))...)
		r.queue.AddRateLimited(ref)
		return true
	}
	r.queue.Forget(ref)
	return true
}

// reconcileOne re-reads the Actor and, if it is still orphaned, re-enters the
// workflow its state names.
func (r *ActorTransitionReconciler) reconcileOne(ctx context.Context, ref resources.ActorRef) error {
	actor, err := r.persistence.GetActor(ctx, ref)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if !r.orphaned(actor) {
		// Finished, or taken up by a request since the list.
		return nil
	}

	state := actor.GetStatus().GetState()
	slog.LogAttrs(ctx, slog.LevelWarn, "Finishing an actor transition whose request was lost",
		append(ateattr.ActorRefLogAttrs(ref),
			slog.String(string(ateattr.ActorStateKey), ateattr.ActorStateValue(state)),
			slog.Time("last_update", actor.GetMetadata().GetUpdateTime().AsTime()))...)

	objRef := &ateapipb.ObjectRef{Atespace: ref.Atespace, Name: ref.Name}
	switch state {
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
		_, err = r.control.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: objRef})
	case ateapipb.ActorState_ACTOR_STATE_PAUSING:
		_, err = r.control.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: objRef})
	}
	if status.Code(err) == codes.Aborted {
		// A client's retry holds the actor lease and finishes it; a later
		// resync finds whatever it leaves.
		return nil
	}
	if err != nil {
		return fmt.Errorf("while finishing %s: %w", ateattr.ActorStateValue(state), err)
	}
	return nil
}
