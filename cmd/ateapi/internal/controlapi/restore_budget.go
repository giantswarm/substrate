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
	"fmt"
	"log/slog"
	"time"

	"go.ate.dev/substrate/internal/ateattr"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Retry policy of the atelet restore within one Resume workflow.
const (
	// restoreAttempts bounds how often the restore is tried before an actor
	// whose restore keeps exceeding its budget is crashed.
	restoreAttempts = 3
	// restoreRetryDelay is the pause before the second attempt; it doubles
	// after each one.
	restoreRetryDelay = 500 * time.Millisecond
)

// restoreTimedOutError is the outcome of a restore whose every attempt
// exceeded its budget. It wraps the last attempt's error.
type restoreTimedOutError struct {
	budget   time.Duration
	attempts int
	err      error
}

func (e *restoreTimedOutError) Error() string {
	return fmt.Sprintf("%s of %s in %d attempt(s): %v", crashMessageRestoreTimedOut, e.budget, e.attempts, e.err)
}

func (e *restoreTimedOutError) Unwrap() error { return e.err }

// crashMessage is the ActorCrash.message of an actor whose restore timed out.
func (e *restoreTimedOutError) crashMessage() string {
	return fmt.Sprintf("%s of %s in %d attempt(s): %s", crashMessageRestoreTimedOut, e.budget, e.attempts, ateletCrashMessage("Restore", e.err))
}

// restoreWithBudget runs the atelet restore one attempt at a time, each under
// w.restoreBudget (none when zero) within the workflow context.
//
// An attempt that exceeds its budget, or that could not reach atelet
// (Unavailable), is tried again while attempts remain and the workflow context
// is live; the layers an interrupted image pull already fetched stay in the
// node's cache, so a retry makes progress rather than starting over. A restore
// whose last attempt exceeded its budget comes back as a restoreTimedOutError.
// Every other failure, and any failure once the workflow context ended, passes
// through untouched.
func (w *ActorWorkflow) restoreWithBudget(ctx context.Context, actorRef resources.ActorRef, restore func(context.Context) error) error {
	delay := restoreRetryDelay
	for attempt := 1; ; attempt++ {
		err := w.restoreAttempt(ctx, restore)
		if err == nil || ctx.Err() != nil {
			return err
		}
		timedOut := w.restoreBudget > 0 && status.Code(err) == codes.DeadlineExceeded
		if !timedOut && status.Code(err) != codes.Unavailable {
			return err
		}
		if attempt >= restoreAttempts {
			if timedOut {
				return &restoreTimedOutError{budget: w.restoreBudget, attempts: attempt, err: err}
			}
			return err
		}
		attrs := append(ateattr.ActorRefLogAttrs(actorRef),
			slog.Int("attempt", attempt),
			slog.Duration("retry_in", delay),
			slog.String(string(ateattr.ErrorTypeKey), status.Code(err).String()),
			slog.Any("err", err),
		)
		slog.LogAttrs(ctx, slog.LevelWarn, "Restore attempt failed; retrying", attrs...)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// restoreAttempt runs one restore under the attempt budget, if there is one.
func (w *ActorWorkflow) restoreAttempt(ctx context.Context, restore func(context.Context) error) error {
	if w.restoreBudget <= 0 {
		return restore(ctx)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, w.restoreBudget)
	defer cancel()
	return restore(attemptCtx)
}
