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
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
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

func requireRestoreTimedOut(t *testing.T, err error, attempts int) {
	t.Helper()
	timedOut, ok := errors.AsType[*restoreTimedOutError](err)
	if !ok {
		t.Fatalf("error = %v, want a restoreTimedOutError", err)
	}
	if timedOut.attempts != attempts {
		t.Fatalf("attempts = %d, want %d (%v)", timedOut.attempts, attempts, err)
	}
	if !strings.Contains(err.Error(), "did not finish within its budget") {
		t.Fatalf("message = %q, want the budget named", err.Error())
	}
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %s, want DeadlineExceeded (%v)", status.Code(err), err)
	}
}

func TestRestoreWithBudget_KeepsExceedingBudget_TimesOut(t *testing.T) {
	w := &ActorWorkflow{restoreBudget: 20 * time.Millisecond, workflowDeadline: time.Minute}
	calls := 0
	err := w.restoreWithBudget(t.Context(), restoreTestActor, func(ctx context.Context) error {
		calls++
		return blockingRestore(ctx)
	})
	requireRestoreTimedOut(t, err, restoreAttempts)
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
			if err := w.restoreWithBudget(t.Context(), restoreTestActor, scriptedRestore(&calls, first, nil)); err != nil {
				t.Fatalf("restoreWithBudget: %v", err)
			}
			if calls != 2 {
				t.Fatalf("restore attempts = %d, want 2", calls)
			}
		})
	}
}

func TestRestoreWithBudget_WorkflowDeadline_TimesOut(t *testing.T) {
	// No attempt budget: the workflow deadline is the only bound, and reaching
	// it during a restore is the terminal outcome, not a retry.
	w := &ActorWorkflow{workflowDeadline: time.Minute}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	err := w.restoreWithBudget(ctx, restoreTestActor, func(ctx context.Context) error {
		calls++
		return blockingRestore(ctx)
	})
	requireRestoreTimedOut(t, err, 1)
	if calls != 1 {
		t.Fatalf("restore attempts = %d, want 1", calls)
	}
}

func TestRestoreWithBudget_PassesThrough(t *testing.T) {
	w := &ActorWorkflow{restoreBudget: 20 * time.Millisecond, workflowDeadline: time.Minute}
	failed := status.Error(codes.Internal, "manifest unknown")
	for name, tc := range map[string]struct {
		ctx     context.Context
		restore func(context.Context) error
		want    error
	}{
		"an unclassified failure": {ctx: t.Context(), restore: func(context.Context) error { return failed }, want: failed},
		"a lost lease": {
			ctx:     func() context.Context { c, cancel := context.WithCancel(t.Context()); cancel(); return c }(),
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
			if _, ok := errors.AsType[*restoreTimedOutError](err); ok {
				t.Fatalf("error was reclassified as a restore timeout: %v", err)
			}
		})
	}
}

// The Restore boundary: a failure crashes the actor with the atelet message, a
// budget exhausted names the budget, a lost lease leaves the actor alone, and
// the crash record lands although the workflow context is past its deadline.
func TestCrashOnRestoreFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		ctx         func(t *testing.T) context.Context
		err         error
		wantCrashed bool
		wantMessage string
	}{
		"nil error": {ctx: func(t *testing.T) context.Context { return t.Context() }},
		"atelet failure": {ctx: func(t *testing.T) context.Context { return t.Context() },
			err:         status.Error(codes.Unknown, "while creating workload from spec: MANIFEST_UNKNOWN: manifest unknown"),
			wantCrashed: true, wantMessage: "atelet Restore: while creating workload from spec: MANIFEST_UNKNOWN: manifest unknown"},
		"budget exhausted past the workflow deadline": {
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
			err:         &restoreTimedOutError{actorRef: restoreTestActor, budget: 90 * time.Second, attempts: 3, err: status.Error(codes.DeadlineExceeded, "context deadline exceeded")},
			wantCrashed: true, wantMessage: "did not finish within its budget of 1m30s in 3 attempt(s)"},
		"lost lease": {
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			err: status.Error(codes.Canceled, "context canceled"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			st, cleanup := storetest.SetupTestStore(t)
			defer cleanup()
			seedActor(t, t.Context(), st, restoreTestActor)
			w := &ActorWorkflow{store: st}
			err := w.crashOnRestoreFailure(tc.ctx(t), restoreTestActor, tc.err)
			if (tc.err == nil) != (err == nil) {
				t.Fatalf("crashOnRestoreFailure() = %v, want error: %v", err, tc.err != nil)
			}
			actor, gerr := st.GetActor(t.Context(), restoreTestActor)
			if gerr != nil {
				t.Fatalf("GetActor: %v", gerr)
			}
			crashed := actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED
			if crashed != tc.wantCrashed {
				t.Fatalf("crashed = %v, want %v (state %s)", crashed, tc.wantCrashed, actor.GetStatus().GetState())
			}
			if tc.wantCrashed && !strings.Contains(actor.GetStatus().GetCrash().GetMessage(), tc.wantMessage) {
				t.Fatalf("crash message = %q, want it to contain %q", actor.GetStatus().GetCrash().GetMessage(), tc.wantMessage)
			}
		})
	}
}
