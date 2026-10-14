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
	"testing"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
)

// otherRecordGoneStore answers the workflow's first store call, the actor
// lease, with a wrapped store.ErrNotFound while the actor's own record stays:
// it stands in for any lookup other than the actor's (its worker, a worker's
// claim) that a concurrent change removed.
type otherRecordGoneStore struct {
	store.Interface
}

func (otherRecordGoneStore) AcquireLease(context.Context, string, string) (*store.Lease, error) {
	return nil, fmt.Errorf("while reading worker %q: %w", "worker-1", store.ErrNotFound)
}

// lifecycleCalls runs each lifecycle RPC whose workflow error lifecycleError
// maps, against an actor in a state the RPC transitions.
var lifecycleCalls = []struct {
	name  string
	state ateapipb.ActorState
	call  func(context.Context, *RPCService, *ateapipb.ObjectRef) error
}{
	{"PauseActor", ateapipb.ActorState_ACTOR_STATE_RUNNING, func(ctx context.Context, s *RPCService, ref *ateapipb.ObjectRef) error {
		_, err := s.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: ref})
		return err
	}},
	{"ResumeActor", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, func(ctx context.Context, s *RPCService, ref *ateapipb.ObjectRef) error {
		_, err := s.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: ref})
		return err
	}},
	{"SuspendActor", ateapipb.ActorState_ACTOR_STATE_RUNNING, func(ctx context.Context, s *RPCService, ref *ateapipb.ObjectRef) error {
		_, err := s.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref})
		return err
	}},
	{"RevertActor", ateapipb.ActorState_ACTOR_STATE_CRASHED, func(ctx context.Context, s *RPCService, ref *ateapipb.ObjectRef) error {
		_, err := s.RevertActor(ctx, &ateapipb.RevertActorRequest{Actor: ref})
		return err
	}},
}

// TestLifecycleRPCs_NotFoundOnlyForMissingActor verifies each lifecycle RPC
// answers NotFound when the actor's own record is gone, and a distinct,
// retryable error when another record its workflow read is gone.
func TestLifecycleRPCs_NotFoundOnlyForMissingActor(t *testing.T) {
	for _, tc := range lifecycleCalls {
		name, call := tc.name, tc.call
		t.Run(name, func(t *testing.T) {
			t.Run("actor gone", func(t *testing.T) {
				ctx := context.Background()
				persistence := newTestPersistence(t)
				svc := &RPCService{
					impl:          newServiceImpl(persistence, nil),
					actorWorkflow: newTestActorWorkflow(t, persistence, "team-a", "tmpl"),
				}

				err := call(ctx, svc, &ateapipb.ObjectRef{Atespace: "team-a", Name: "missing"})
				if got := apierror.Code(err); got != codes.NotFound {
					t.Fatalf("%s = %v (code %v), want NotFound", name, err, got)
				}
				if errors.Is(err, errLifecycleRecordGone) {
					t.Errorf("%s = %v, want no errLifecycleRecordGone for a missing actor", name, err)
				}
			})

			t.Run("other record gone", func(t *testing.T) {
				ctx := context.Background()
				persistence := newTestPersistence(t)
				w := newTestActorWorkflow(t, persistence, "team-a", "tmpl")
				w.store = otherRecordGoneStore{Interface: persistence}
				svc := &RPCService{impl: newServiceImpl(persistence, nil), actorWorkflow: w}
				actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
				seedWorkflowActor(t, ctx, persistence, actorRef, "team-a", "tmpl", tc.state)

				err := call(ctx, svc, &ateapipb.ObjectRef{Atespace: actorRef.Atespace, Name: actorRef.Name})
				if got := apierror.Code(err); got != codes.Aborted {
					t.Fatalf("%s = %v (code %v), want Aborted", name, err, got)
				}
				if !errors.Is(err, errLifecycleRecordGone) {
					t.Errorf("%s = %v, want errLifecycleRecordGone", name, err)
				}
				if errors.Is(err, store.ErrNotFound) {
					t.Errorf("%s = %v, still matches store.ErrNotFound", name, err)
				}
			})
		})
	}
}
