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

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func shortenActorLeaseWait(t *testing.T, wait time.Duration) {
	t.Helper()
	oldWait, oldRetry := actorLeaseWait, actorLeaseRetry
	actorLeaseWait, actorLeaseRetry = wait, time.Millisecond
	t.Cleanup(func() { actorLeaseWait, actorLeaseRetry = oldWait, oldRetry })
}

var leaseHeld = status.Error(codes.Aborted, "another operation is in progress for this actor")

func TestAwaitActorLease(t *testing.T) {
	actor := &ateapipb.ObjectRef{Atespace: "demo", Name: "a"}

	t.Run("waits out a held lease", func(t *testing.T) {
		shortenActorLeaseWait(t, time.Minute)
		calls := 0
		got, err := AwaitActorLease(t, context.Background(), "PauseActor", actor, func() (string, error) {
			calls++
			if calls < 3 {
				return "", leaseHeld
			}
			return "paused", nil
		})
		if err != nil || got != "paused" || calls != 3 {
			t.Fatalf("got (%q, %v) after %d calls, want (\"paused\", nil) after 3", got, err, calls)
		}
	})

	t.Run("returns any other error at once", func(t *testing.T) {
		shortenActorLeaseWait(t, time.Minute)
		calls := 0
		_, err := AwaitActorLease(t, context.Background(), "PauseActor", actor, func() (string, error) {
			calls++
			return "", status.Error(codes.FailedPrecondition, "not running")
		})
		if status.Code(err) != codes.FailedPrecondition || calls != 1 {
			t.Fatalf("got %v after %d calls, want FailedPrecondition after 1", err, calls)
		}
	})

	t.Run("a lease held past the budget fails clearly", func(t *testing.T) {
		shortenActorLeaseWait(t, 20*time.Millisecond)
		_, err := AwaitActorLease(t, context.Background(), "SuspendActor", actor, func() (string, error) {
			return "", leaseHeld
		})
		if status.Code(err) != codes.Aborted {
			t.Fatalf("got %v, want the Aborted error wrapped", err)
		}
		if !strings.Contains(err.Error(), "SuspendActor demo/a: the actor was still busy with another operation after") {
			t.Fatalf("error %q does not say the wait was spent", err)
		}
	})

	t.Run("stops when the context ends", func(t *testing.T) {
		shortenActorLeaseWait(t, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		_, err := AwaitActorLease(t, ctx, "PauseActor", actor, func() (string, error) {
			cancel()
			return "", leaseHeld
		})
		if err != context.Canceled {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
}
