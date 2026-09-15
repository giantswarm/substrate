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
	"github.com/agent-substrate/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type leaseStore struct{ store.Interface }

func (leaseStore) AcquireLease(ctx context.Context, _ string) (*store.Lease, error) {
	return store.NewLease(ctx, func() {}), nil
}

func TestAcquireActorLeaseWorkflowDeadline(t *testing.T) {
	w := &ActorWorkflow{store: leaseStore{}, workflowDeadline: 20 * time.Millisecond}

	ctx, lease, err := w.acquireActorLease(context.Background(), resources.ActorRef{Atespace: "space", Name: "actor"})
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

// TestAcquireActorLeaseWaitsOutABriefHolder verifies a workflow waits for a
// lease another holder has for a moment — the background commit of a pause
// snapshot's durable copy — instead of answering Aborted, and that a lease
// held for good is still a conflict.
func TestAcquireActorLeaseWaitsOutABriefHolder(t *testing.T) {
	ctx := context.Background()
	persistence := newTestPersistence(t)
	actorRef := resources.ActorRef{Atespace: "team-a", Name: "actor-1"}
	w := &ActorWorkflow{store: persistence, workflowDeadline: time.Minute}

	t.Run("released while waiting", func(t *testing.T) {
		held, err := persistence.AcquireLease(ctx, actorLeaseKey(actorRef))
		if err != nil {
			t.Fatalf("AcquireLease: %v", err)
		}
		go func() {
			time.Sleep(60 * time.Millisecond)
			held.Close()
		}()

		start := time.Now()
		_, lease, err := w.acquireActorLease(ctx, actorRef)
		if err != nil {
			t.Fatalf("acquireActorLease while the holder released after 60 ms: %v", err)
		}
		lease.Close()
		if waited := time.Since(start); waited < 50*time.Millisecond {
			t.Errorf("acquired after %s, want to have waited for the holder", waited)
		}
	})

	t.Run("held for good", func(t *testing.T) {
		held, err := persistence.AcquireLease(ctx, actorLeaseKey(actorRef))
		if err != nil {
			t.Fatalf("AcquireLease: %v", err)
		}
		defer held.Close()

		_, _, err = w.acquireActorLease(ctx, actorRef)
		if got := status.Code(err); got != codes.Aborted {
			t.Fatalf("acquireActorLease under a held lease = %v, want Aborted", err)
		}
	})
}
