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
	"log/slog"
	"time"

	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/resources"
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
	// crashRecordTimeout bounds the store writes that record a crash whose
	// cause was the workflow deadline itself.
	crashRecordTimeout = 30 * time.Second
)

// restoreTimedOutError is the outcome of a restore that kept exceeding its
// budget: every attempt used up, or the workflow deadline reached while one
// ran. It wraps the last attempt's error.
type restoreTimedOutError struct {
	actorRef resources.ActorRef
	budget   time.Duration
	attempts int
	err      error
}

func (e *restoreTimedOutError) Error() string {
	return fmt.Sprintf("restore of actor %s did not finish within its budget of %s in %d attempt(s): %v", e.actorRef, e.budget, e.attempts, e.err)
}

func (e *restoreTimedOutError) Unwrap() error { return e.err }

// restoreWithBudget runs the atelet restore one attempt at a time, each under
// w.restoreBudget (none when zero) within the workflow context.
//
// An attempt that exceeds its budget, or that could not reach atelet
// (Unavailable: the worker pod's atelet restarting), is tried again while
// attempts and the workflow deadline remain; the layers an interrupted image
// pull already fetched stay in the node's cache, so a retry makes progress
// rather than starting over. A restore that keeps exceeding its budget comes
// back as a restoreTimedOutError, which crashOnRestoreFailure records as the
// actor's crash.
//
// A workflow context cancelled for any other reason (the lease was lost) ends
// the attempts with the error as it is: another workflow owns the actor now.
// Every other failure passes through untouched.
func (w *ActorWorkflow) restoreWithBudget(ctx context.Context, actorRef resources.ActorRef, restore func(context.Context) error) error {
	delay := restoreRetryDelay
	for attempt := 1; ; attempt++ {
		err := w.restoreAttempt(ctx, restore)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return err
		}
		timedOut := status.Code(err) == codes.DeadlineExceeded
		if !timedOut && status.Code(err) != codes.Unavailable {
			return err
		}
		last := attempt >= restoreAttempts || ctx.Err() != nil
		if timedOut && last {
			budget := w.restoreBudget
			if budget <= 0 {
				budget = w.workflowDeadline
			}
			return &restoreTimedOutError{actorRef: actorRef, budget: budget, attempts: attempt, err: err}
		}
		if last {
			return err
		}
		attrs := ateattr.ActorRefLogAttrs(actorRef)
		attrs = append(attrs,
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

// crashOnRestoreFailure is the Restore boundary of the Resume workflow: a nil
// err is a nil return; a workflow context cancelled by a lost lease leaves the
// actor to the workflow that owns it now; a restore that kept exceeding its
// budget, or ran into the workflow deadline, crashes the actor; any other
// failure is classified as every atelet error is (handleAteletError). The crash
// record is written detached from the workflow context, under its own bound, so
// it lands when the failure was the workflow deadline itself; a restore that
// kept exceeding its budget names the budget in its message.
func (w *ActorWorkflow) crashOnRestoreFailure(ctx context.Context, actorRef resources.ActorRef, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("while restoring actor %s: %w", actorRef, err)
	}
	timedOut, budgetExceeded := errors.AsType[*restoreTimedOutError](err)
	if !budgetExceeded && ctx.Err() == nil {
		return handleAteletError(ctx, w.store, actorRef, ateattr.OperationResume, "Restore", false, err)
	}
	message := ateletCrashMessage("Restore", err)
	if budgetExceeded {
		message = timedOut.Error()
	}
	slog.LogAttrs(ctx, slog.LevelError, "Setting Actor to crashed due to error",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.Any("err", err))...)
	crashCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), crashRecordTimeout)
	defer cancel()
	if cerr := crashActor(crashCtx, w.store, actorRef, ateattr.OperationResume, message); cerr != nil {
		return cerr
	}
	return fmt.Errorf("actor %s crashed: %w", actorRef, err)
}
