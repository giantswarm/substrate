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
	"log/slog"
	"strings"
	"time"

	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/ateattr"
	"go.ate.dev/substrate/internal/preview"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
)

// Retry policy of a resume that a rolling worker pool interrupts, within one
// ResumeActor call.
const (
	// transientResumeAttempts bounds the resumes one call makes. A call that
	// uses them up leaves the actor at rest for the caller's retry.
	transientResumeAttempts = 6
	// transientResumeRetryDelay is the pause before the second resume; it
	// doubles after each one, up to transientResumeRetryMaxDelay.
	transientResumeRetryDelay    = time.Second
	transientResumeRetryMaxDelay = 15 * time.Second
	// crashRecordTimeout bounds the store reads that answer for a crash after
	// the caller's context ended.
	crashRecordTimeout = 30 * time.Second
)

// transientCrashCauses are the crash messages a rolling worker pool produces:
// the actor's worker drained, its pod went away, or its worker process
// restarted. None of them says anything about the actor itself.
var transientCrashCauses = []string{
	crashMessageWorkerDraining,
	crashMessageWorkerPodGone,
	crashMessageWorkerRestarted,
}

// errRevertNotAccepted is revertActor's refusal of an actor its accept
// function rejects.
var errRevertNotAccepted = errors.New("actor is not in the state the revert was asked for")

// transientCrashCause returns the part of actor's crash message after the
// "<operation> failed: " prefix, and the operation, when actor is CRASHED for
// a cause in transientCrashCauses; ok is false otherwise.
func transientCrashCause(actor *ateapi.Actor) (op, cause string, ok bool) {
	if actor.GetStatus().GetState() != ateapi.ActorState_ACTOR_STATE_CRASHED {
		return "", "", false
	}
	cause = actor.GetStatus().GetCrash().GetMessage()
	if before, rest, found := strings.Cut(cause, " failed: "); found {
		op, cause = before, rest
	}
	for _, c := range transientCrashCauses {
		if strings.HasPrefix(cause, c) {
			return op, cause, true
		}
	}
	return "", "", false
}

// transientCrash reports whether actor is CRASHED for a cause in
// transientCrashCauses, whatever operation it was recorded under.
func transientCrash(actor *ateapi.Actor) bool {
	_, _, ok := transientCrashCause(actor)
	return ok
}

// recoverableCrash reports whether actor, crashed for a transient cause, can
// be returned to the snapshot it would resume from without losing any data.
// That snapshot must be the latest COMPLETED durable one (or the actor has
// none and cold-boots), so reverting the actor, which drops node-local
// copies, lands on that same snapshot; and the crash must have cut a resume,
// so the actor never ran past it. A REVERTING actor is accepted under the
// same snapshot rule: a revert that did not finish lands on that snapshot.
func recoverableCrash(actor *ateapi.Actor) bool {
	status := actor.GetStatus()
	if snapshotToResume(status) != findLatestDurableSnapshot(status, ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED) {
		return false
	}
	if status.GetState() == ateapi.ActorState_ACTOR_STATE_REVERTING {
		return true
	}
	op, _, ok := transientCrashCause(actor)
	return ok && op == ateattr.OperationResume
}

// ResumeActor resumes a suspended actor (see resumeActorOnce). An actor a
// rolling worker pool crashed (transientCrash), during this resume or before
// it, is returned to its latest durable snapshot when that loses nothing
// (recoverableCrash), its worker torn down and freed and its crash cleared,
// and resumed again with backoff on another worker, waiting for one while the
// rolling pool has none free. A call whose attempts or context run out
// answers Unavailable naming the crash, so the caller's retry resumes the
// actor. Any other failure is returned as it is.
func (w *ActorWorkflow) ResumeActor(ctx context.Context, actorRef resources.ActorRef) (*ateapi.Actor, bool, error) {
	ctx = preview.ContextWithOptIn(ctx)
	delay := transientResumeRetryDelay
	// crash is the last transient crash this call met: once one is known,
	// the pool is rolling and a resume that finds no free worker waits for
	// one with the same backoff.
	var crash string
	for attempt := 1; ; attempt++ {
		actor, resumed, err := w.resumeActorOnce(ctx, actorRef)
		if err == nil {
			return actor, resumed, nil
		}
		metCrash, rerr := w.recoverTransientCrash(ctx, actorRef)
		switch {
		case metCrash != "":
			crash = metCrash
			if rerr != nil {
				slog.LogAttrs(ctx, slog.LevelWarn, "Failed to recover an actor from a transient crash; retrying",
					append(ateattr.ActorRefLogAttrs(actorRef), slog.Any("err", rerr))...)
			}
		case crash != "" && apierror.Code(err) == codes.ResourceExhausted:
		default:
			return nil, false, err
		}
		retryable := apierror.Unavailable("resume of actor %s was interrupted by its worker (%s); the actor returns to its snapshot, retry the resume", actorRef, crash)
		if attempt >= transientResumeAttempts {
			return nil, false, retryable
		}
		slog.LogAttrs(ctx, slog.LevelWarn, "Resume interrupted by a transient worker failure; retrying",
			append(ateattr.ActorRefLogAttrs(actorRef),
				slog.Int("attempt", attempt),
				slog.Duration("retry_in", delay),
				slog.String("crash", crash))...)
		select {
		case <-ctx.Done():
			return nil, false, retryable
		case <-time.After(delay):
		}
		delay = min(2*delay, transientResumeRetryMaxDelay)
	}
}

// recoverTransientCrash reverts an actor that recoverableCrash accepts, as
// read under the actor lease, to its latest durable snapshot. It returns the
// crash message whenever the actor was found recoverable, even when the
// revert then fails or finds it reverted already; an actor in any other state
// is left alone and yields "".
func (w *ActorWorkflow) recoverTransientCrash(ctx context.Context, actorRef resources.ActorRef) (crash string, err error) {
	// Read beyond the caller's context, as the crash itself is recorded, so a
	// call that ends under its crash still answers for it.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), crashRecordTimeout)
	actor, err := w.store.GetActor(readCtx, actorRef)
	cancel()
	if err != nil || !recoverableCrash(actor) {
		return "", nil
	}
	crash = actor.GetStatus().GetCrash().GetMessage()
	if crash == "" {
		crash = "an unfinished revert"
	}
	if _, err := w.revertActor(ctx, actorRef, recoverableCrash); err != nil {
		if errors.Is(err, errRevertNotAccepted) {
			return crash, nil
		}
		return crash, err
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "Recovered actor from a transient crash",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.String("crash", crash))...)
	return crash, nil
}
