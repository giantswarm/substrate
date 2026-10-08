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
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type leaseStore struct{ store.Interface }

func (leaseStore) AcquireLease(ctx context.Context, _, _ string) (*store.Lease, error) {
	return store.NewLease(ctx, func() {}), nil
}

func TestAcquireActorLeaseWorkflowDeadline(t *testing.T) {
	w := &ActorWorkflow{store: leaseStore{}, workflowDeadline: 20 * time.Millisecond}

	ctx, lease, err := w.acquireActorLease(context.Background(), resources.ActorRef{Atespace: "space", Name: "actor"}, ateattr.OperationResume)
	if err != nil {
		t.Fatalf("acquireActorLease: %v", err)
	}
	t.Cleanup(lease.Close)

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error = %v, want DeadlineExceeded", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("workflow context did not reach its deadline")
	}
}

func TestAcquireActorLeaseOutlivesCallerCancellation(t *testing.T) {
	w := &ActorWorkflow{store: leaseStore{}, workflowDeadline: 200 * time.Millisecond}

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	ctx, lease, err := w.acquireActorLease(callerCtx, resources.ActorRef{Atespace: "space", Name: "actor"}, ateattr.OperationResume)
	if err != nil {
		t.Fatalf("acquireActorLease: %v", err)
	}
	t.Cleanup(lease.Close)

	// The caller gives up: the workflow context stays alive until its own
	// deadline, which is what ends it.
	cancelCaller()
	select {
	case <-ctx.Done():
		t.Fatalf("workflow context ended with the caller's cancellation: %v", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error = %v, want DeadlineExceeded", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("workflow context did not reach its deadline")
	}
}

// TestAcquireActorLeaseHeldIsAborted verifies two lifecycle operations on one
// actor are a conflict: the second finds the lease held and is Aborted at
// once, without waiting on the first.
func TestAcquireActorLeaseHeldIsAborted(t *testing.T) {
	ctx := context.Background()
	persistence := newTestPersistence(t)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	w := &ActorWorkflow{store: persistence, workflowDeadline: time.Minute}

	held, err := persistence.AcquireLease(ctx, actorLeaseKey(actorRef), ateattr.OperationResume)
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	defer held.Close()

	start := time.Now()
	_, _, err = w.acquireActorLease(ctx, actorRef, ateattr.OperationPause)
	if got := status.Code(err); got != codes.Aborted {
		t.Fatalf("acquireActorLease under a held lease = %v, want Aborted", err)
	}
	if waited := time.Since(start); waited > 500*time.Millisecond {
		t.Errorf("Aborted after %s, want at once", waited)
	}
}
