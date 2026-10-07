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
	"fmt"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// actorLeaseWait bounds how long AwaitActorLease waits for another
// operation on the actor to finish. The background commit of a pause
// snapshot's durable copy takes the actor's lease after the pause returns;
// on a loaded runner it can outlast the second or so the control plane
// retries a held lease before answering Aborted. Variables so unit tests can
// shorten them.
var (
	actorLeaseWait  = 30 * time.Second
	actorLeaseRetry = time.Second
)

// AwaitActorLease runs a lifecycle call (pause, suspend) on the actor,
// retrying while the control plane answers Aborted because another operation
// holds the actor's lease. Aborted asks the caller to retry; a client's next
// operation right after a pause meets it whenever the pause's background
// upload commits at that moment. Any other error is returned as it is; a
// lease still held once the wait budget is spent fails with a message that
// says so. Each retry is logged so a pass that had to wait stays visible.
func AwaitActorLease[T any](t *testing.T, ctx context.Context, op string, actor *ateapipb.ObjectRef, call func() (T, error)) (T, error) {
	t.Helper()
	start := time.Now()
	for {
		resp, err := call()
		if status.Code(err) != codes.Aborted {
			return resp, err
		}
		if waited := time.Since(start); waited >= actorLeaseWait {
			return resp, fmt.Errorf("%s %s/%s: the actor was still busy with another operation after %s: %w", op, actor.GetAtespace(), actor.GetName(), waited.Round(time.Second), err)
		}
		t.Logf("%s %s/%s: another operation holds the actor (%v); retrying", op, actor.GetAtespace(), actor.GetName(), err)
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-time.After(actorLeaseRetry):
		}
	}
}
