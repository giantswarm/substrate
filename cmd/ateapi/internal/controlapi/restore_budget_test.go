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

	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/agent-substrate/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var restoreTestActor = resources.ActorRef{Atespace: "space", Name: "actor"}

// blockingRestore is a restore that never finishes on its own: it returns the
// gRPC status a client call reports once its context ends.
func blockingRestore(ctx context.Context) error {
	<-ctx.Done()
	return status.FromContextError(ctx.Err()).Err()
}

// scriptedRestore runs the given outcomes in order; nil means success.
func scriptedRestore(calls *int, outcomes ...func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		i := *calls
		*calls++
		if i >= len(outcomes) || outcomes[i] == nil {
			return nil
		}
		return outcomes[i](ctx)
	}
}

func statusRestore(code codes.Code) func(context.Context) error {
	return func(context.Context) error { return status.Error(code, code.String()) }
}

func requireRestoreTimedOut(t *testing.T, err error) {
	t.Helper()
	if !ateerrors.ActorCrashRequested(err) {
		t.Fatalf("error does not carry the crash directive: %v", err)
	}
	if got := ateattr.FailureReason(err); got != ateattr.ReasonRestoreTimedOut {
		t.Fatalf("failure reason = %q, want %q (%v)", got, ateattr.ReasonRestoreTimedOut, err)
	}
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %s, want DeadlineExceeded (%v)", status.Code(err), err)
	}
}

func TestRestoreWithBudget_KeepsExceedingBudget_FailsActor(t *testing.T) {
	w := &ActorWorkflow{restoreBudget: 20 * time.Millisecond, workflowDeadline: time.Minute}
	calls := 0
	err := w.restoreWithBudget(context.Background(), restoreTestActor, func(ctx context.Context) error {
		calls++
		return blockingRestore(ctx)
	})
	requireRestoreTimedOut(t, err)
	if calls != restoreAttempts {
		t.Fatalf("restore attempts = %d, want %d", calls, restoreAttempts)
	}
}

func TestRestoreWithBudget_RetrySucceeds(t *testing.T) {
	w := &ActorWorkflow{restoreBudget: 20 * time.Millisecond, workflowDeadline: time.Minute}
	for name, first := range map[string]func(context.Context) error{
		"after a timed-out attempt":   blockingRestore,
		"after an unavailable atelet": statusRestore(codes.Unavailable),
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			if err := w.restoreWithBudget(context.Background(), restoreTestActor, scriptedRestore(&calls, first, nil)); err != nil {
				t.Fatalf("restoreWithBudget: %v", err)
			}
			if calls != 2 {
				t.Fatalf("restore attempts = %d, want 2", calls)
			}
		})
	}
}

func TestRestoreWithBudget_WorkflowDeadline_FailsActor(t *testing.T) {
	// No attempt budget: the workflow deadline is the only bound, and reaching
	// it during a restore is the terminal outcome, not a retry.
	w := &ActorWorkflow{workflowDeadline: time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	err := w.restoreWithBudget(ctx, restoreTestActor, func(ctx context.Context) error {
		calls++
		return blockingRestore(ctx)
	})
	requireRestoreTimedOut(t, err)
	if calls != 1 {
		t.Fatalf("restore attempts = %d, want 1", calls)
	}
}

func TestRestoreWithBudget_PassesThrough(t *testing.T) {
	w := &ActorWorkflow{restoreBudget: 20 * time.Millisecond, workflowDeadline: time.Minute}
	crashed := ateerrors.NewGRPCError(context.Background(), codes.DataLoss, ateerrors.ReasonFailedGetExternalObject, ateerrors.ActorCrashedMetadata(), errors.New("manifest unknown"))
	for name, tc := range map[string]struct {
		ctx     context.Context
		restore func(context.Context) error
		want    error
	}{
		"an unclassified failure":      {ctx: context.Background(), restore: statusRestore(codes.Internal)},
		"atelet's own crash directive": {ctx: context.Background(), restore: func(context.Context) error { return crashed }, want: crashed},
		"a lost lease": {
			ctx:     func() context.Context { c, cancel := context.WithCancel(context.Background()); cancel(); return c }(),
			restore: blockingRestore,
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			err := w.restoreWithBudget(tc.ctx, restoreTestActor, scriptedRestore(&calls, tc.restore))
			if err == nil {
				t.Fatal("restoreWithBudget returned nil")
			}
			if calls != 1 {
				t.Fatalf("restore attempts = %d, want 1", calls)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v untouched", err, tc.want)
			}
			if ateattr.FailureReason(err) == ateattr.ReasonRestoreTimedOut {
				t.Fatalf("error was reclassified as RESTORE_TIMED_OUT: %v", err)
			}
		})
	}
}
