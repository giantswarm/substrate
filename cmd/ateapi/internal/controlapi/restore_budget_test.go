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
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var restoreTestActor = resources.ActorRef{Atespace: "space", Name: "actor"}

// blockingRestore never finishes on its own: it returns the gRPC status a
// client call reports once its context ends.
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

func TestRestoreWithBudgetKeepsExceedingBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &ActorWorkflow{restoreBudget: 90 * time.Second}
		calls := 0
		err := w.restoreWithBudget(t.Context(), restoreTestActor, func(ctx context.Context) error {
			calls++
			return blockingRestore(ctx)
		})
		timedOut, ok := errors.AsType[*restoreTimedOutError](err)
		if !ok {
			t.Fatalf("error = %v, want a restoreTimedOutError", err)
		}
		if timedOut.attempts != restoreAttempts || calls != restoreAttempts {
			t.Fatalf("attempts = %d, calls = %d, want %d", timedOut.attempts, calls, restoreAttempts)
		}
		if msg := timedOut.crashMessage(); !strings.HasPrefix(msg, crashMessageRestoreTimedOut+" of 1m30s in 3 attempt(s): atelet Restore: ") {
			t.Fatalf("crash message = %q", msg)
		}
		if len(timedOut.crashMessage()) > maxCrashMessageBytes {
			t.Fatalf("crash message is longer than %d bytes", maxCrashMessageBytes)
		}
	})
}

func TestRestoreWithBudgetRetrySucceeds(t *testing.T) {
	for name, first := range map[string]func(context.Context) error{
		"after a timed-out attempt":   blockingRestore,
		"after an unavailable atelet": statusRestore(codes.Unavailable),
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := &ActorWorkflow{restoreBudget: time.Minute}
				calls := 0
				if err := w.restoreWithBudget(t.Context(), restoreTestActor, scriptedRestore(&calls, first, nil)); err != nil {
					t.Fatalf("restoreWithBudget: %v", err)
				}
				if calls != 2 {
					t.Fatalf("restore attempts = %d, want 2", calls)
				}
			})
		})
	}
}

func TestRestoreWithBudgetPassesThrough(t *testing.T) {
	failed := status.Error(codes.Internal, "manifest unknown")
	for name, tc := range map[string]struct {
		budget  time.Duration
		ctx     func(t *testing.T) context.Context
		restore func(context.Context) error
		want    error
	}{
		"an unclassified failure": {
			budget:  time.Minute,
			ctx:     func(t *testing.T) context.Context { return t.Context() },
			restore: func(context.Context) error { return failed },
			want:    failed,
		},
		"a lost lease": {
			budget: time.Minute,
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			restore: blockingRestore,
		},
		"the workflow deadline without an attempt budget": {
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				t.Cleanup(cancel)
				return ctx
			},
			restore: blockingRestore,
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := &ActorWorkflow{restoreBudget: tc.budget}
				calls := 0
				err := w.restoreWithBudget(tc.ctx(t), restoreTestActor, scriptedRestore(&calls, tc.restore))
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
		})
	}
}
