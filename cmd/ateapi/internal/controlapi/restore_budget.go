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
	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/agent-substrate/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Retry policy of the atelet restore within one Resume workflow.
const (
	// restoreAttempts bounds how often the restore is tried before an actor
	// whose restore keeps exceeding its budget is failed.
	restoreAttempts = 3
	// restoreRetryDelay is the pause before the second attempt; it doubles
	// after each one.
	restoreRetryDelay = 500 * time.Millisecond
)

// restoreWithBudget runs the atelet restore one attempt at a time, each under
// w.restoreBudget (none when zero) within the workflow context.
//
// An attempt that exceeds its budget, or that could not reach atelet
// (Unavailable: the worker pod's atelet restarting), is tried again while
// attempts and the workflow deadline remain; the layers an interrupted image
// pull already fetched stay in the node's cache, so a retry makes progress
// rather than starting over. A restore that keeps exceeding its budget --
// every attempt used up, or the workflow deadline reached while one ran --
// comes back tagged RESTORE_TIMED_OUT with the crash directive, so the Restore
// boundary (maybeCrashActor) fails the actor with that reason instead of
// leaving it RESUMING on a claimed worker, where nothing reclaims it and
// DeleteActor refuses it.
//
// A workflow context cancelled for any other reason -- the lease was lost --
// ends the attempts with the error as it is: another workflow owns the actor
// now. Every other failure passes through untouched: atelet's own crash
// directives keep their reason, and an error atelet did not classify stays the
// caller's to retry, as before.
func (w *ActorWorkflow) restoreWithBudget(ctx context.Context, actorRef resources.ActorRef, restore func(context.Context) error) error {
	delay := restoreRetryDelay
	for attempt := 1; ; attempt++ {
		err := w.restoreAttempt(ctx, restore)
		if err == nil || ateerrors.ActorCrashRequested(err) {
			return err
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
			return ateerrors.NewGRPCError(ctx, codes.DeadlineExceeded, ateerrors.ReasonRestoreTimedOut, ateerrors.ActorCrashedMetadata(),
				fmt.Errorf("restore of actor %s did not finish within its budget of %s in %d attempt(s): %w", actorRef, budget, attempt, err))
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
