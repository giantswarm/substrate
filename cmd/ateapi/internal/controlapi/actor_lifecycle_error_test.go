// Copyright 2026 The Agent Substrate Authors
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

	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
)

// otherRecordGoneStore answers the actor lease with a wrapped
// store.ErrNotFound while the actor's own record stays: it stands in for any
// lookup other than the actor's (its worker, a worker's assignment) that a
// concurrent change removed.
type otherRecordGoneStore struct {
	store.Interface
}

func (otherRecordGoneStore) AcquireLease(context.Context, string) (*store.Lease, error) {
	return nil, fmt.Errorf("while reading worker %q: %w", "worker-1", store.ErrNotFound)
}

// lifecycleCalls runs each lifecycle RPC whose workflow error lifecycleError
// maps, against an actor in a state the RPC transitions.
var lifecycleCalls = []struct {
	name  string
	state ateapi.ActorState
	call  func(context.Context, *RPCService, *ateapi.ObjectRef) error
}{
	{"ResumeActor", ateapi.ActorState_ACTOR_STATE_SUSPENDED, func(ctx context.Context, s *RPCService, ref *ateapi.ObjectRef) error {
		_, err := s.ResumeActor(ctx, &ateapi.ResumeActorRequest{Actor: ref})
		return err
	}},
	{"SuspendActor", ateapi.ActorState_ACTOR_STATE_RUNNING, func(ctx context.Context, s *RPCService, ref *ateapi.ObjectRef) error {
		_, err := s.SuspendActor(ctx, &ateapi.SuspendActorRequest{Actor: ref})
		return err
	}},
	{"RevertActor", ateapi.ActorState_ACTOR_STATE_CRASHED, func(ctx context.Context, s *RPCService, ref *ateapi.ObjectRef) error {
		_, err := s.RevertActor(ctx, &ateapi.RevertActorRequest{Actor: ref})
		return err
	}},
}

// TestLifecycleRPCsNotFoundOnlyForMissingActor verifies each lifecycle RPC
// answers NotFound when the actor's own record is gone, and a distinct,
// retryable error when another record its workflow read is gone.
func TestLifecycleRPCsNotFoundOnlyForMissingActor(t *testing.T) {
	for _, tc := range lifecycleCalls {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("actor gone", func(t *testing.T) {
				ctx := t.Context()
				persistence := newTestPersistence(t)
				svc := &RPCService{
					impl:          newServiceImpl(persistence, nil),
					actorWorkflow: newTestActorWorkflow(t, persistence, "team-a", "tmpl"),
				}

				err := tc.call(ctx, svc, &ateapi.ObjectRef{Atespace: "team-a", Name: "missing"})
				if got := apierror.Code(err); got != codes.NotFound {
					t.Fatalf("%s = %v (code %v), want NotFound", tc.name, err, got)
				}
				if errors.Is(err, errLifecycleRecordGone) {
					t.Errorf("%s = %v, want no errLifecycleRecordGone for a missing actor", tc.name, err)
				}
			})

			t.Run("other record gone", func(t *testing.T) {
				ctx := t.Context()
				persistence := newTestPersistence(t)
				w := newTestActorWorkflow(t, persistence, "team-a", "tmpl")
				w.store = otherRecordGoneStore{Interface: persistence}
				svc := &RPCService{impl: newServiceImpl(persistence, nil), actorWorkflow: w}
				actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
				seedWorkflowActor(t, ctx, persistence, actorRef, "team-a", "tmpl", tc.state)

				err := tc.call(ctx, svc, &ateapi.ObjectRef{Atespace: actorRef.Atespace, Name: actorRef.Name})
				if got := apierror.Code(err); got != codes.Aborted {
					t.Fatalf("%s = %v (code %v), want Aborted", tc.name, err, got)
				}
				if !errors.Is(err, errLifecycleRecordGone) {
					t.Errorf("%s = %v, want errLifecycleRecordGone", tc.name, err)
				}
				if errors.Is(err, store.ErrNotFound) {
					t.Errorf("%s = %v, still matches store.ErrNotFound", tc.name, err)
				}
			})
		})
	}
}
