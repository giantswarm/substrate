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
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
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

// sandboxNetworkNotTaken is the worker's answer to a boot whose sandbox did
// not take the actor's network (actornet.SandboxNetworkNotTaken), which an
// atelet error quotes. The cause is the worker's: the workload never got an
// interface to answer on, so the next boot outlives it.
const sandboxNetworkNotTaken = "the sandbox did not take the actor's network"

// errRevertNotAccepted is revertActor's refusal of an actor its accept
// function rejects.
var errRevertNotAccepted = errors.New("actor is not in the state the revert was asked for")

// transientCrashCause returns the part of actor's crash message after the
// "<operation> failed: " prefix, and the operation, when the actor's crash
// record names a cause in transientCrashCauses; ok is false otherwise.
func transientCrashCause(actor *ateapi.Actor) (op, cause string, ok bool) {
	cause = actor.GetStatus().GetCrash().GetMessage()
	if before, rest, found := strings.Cut(cause, " failed: "); found {
		op, cause = before, rest
	}
	for _, c := range transientCrashCauses {
		if strings.HasPrefix(cause, c) {
			return op, cause, true
		}
	}
	if strings.Contains(cause, sandboxNetworkNotTaken) {
		return op, cause, true
	}
	return "", "", false
}

// transientCrash reports whether actor is CRASHED for a cause in
// transientCrashCauses, whatever operation it was recorded under.
func transientCrash(actor *ateapi.Actor) bool {
	_, _, ok := transientCrashCause(actor)
	return ok && actor.GetStatus().GetState() == ateapi.ActorState_ACTOR_STATE_CRASHED
}

// recoverableCrash reports whether actor, crashed for a transient cause, can
// be returned to the snapshot it would resume from without losing anything
// the crash has not lost already. That snapshot must be the latest COMPLETED
// durable one (or the actor has none and cold-boots), so reverting the actor,
// which drops node-local copies and discards a half-written snapshot, lands
// on that same snapshot. Then:
//
//   - a crash that cut a resume is recoverable: the actor never ran past
//     that snapshot;
//   - a crash that took a running workload, or a suspend in progress, is
//     recoverable only onto a snapshot the actor wrote itself: the workload
//     went with its worker, so that snapshot is the latest state of the
//     actor that survives. One that holds only its template's or tag's
//     snapshot, or none, stays CRASHED, since returning it there would reset
//     it.
//
// A REVERTING actor that still holds such a crash record is a recovery that
// did not finish, and is accepted under the same rules.
func recoverableCrash(actor *ateapi.Actor) bool {
	status := actor.GetStatus()
	switch status.GetState() {
	case ateapi.ActorState_ACTOR_STATE_CRASHED, ateapi.ActorState_ACTOR_STATE_REVERTING:
	default:
		return false
	}
	snap := snapshotToResume(status)
	if snap != findLatestDurableSnapshot(status, ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED) {
		return false
	}
	op, _, ok := transientCrashCause(actor)
	switch {
	case !ok:
		return false
	case op == ateattr.OperationResume:
		return true
	default:
		return snap.GetOwner() == ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR
	}
}

// ResumeActor resumes a suspended actor (see resumeActorOnce). An actor a
// rolling worker pool crashed (transientCrash), during this resume or before
// it, while it ran or while it was suspended, is returned to its latest
// durable snapshot when recoverableCrash allows it, its worker torn down and freed and its crash cleared,
// and resumed again with backoff on another worker, waiting for one while the
// rolling pool has none free. A call whose attempts or context run out
// answers Unavailable naming the crash, so the caller's retry resumes the
// actor. Any other failure is returned as it is.
func (w *ActorWorkflow) ResumeActor(ctx context.Context, actorRef resources.ActorRef, token *ateapi.FencingToken) (*ateapi.Actor, bool, error) {
	ctx = preview.ContextWithOptIn(ctx)
	delay := transientResumeRetryDelay
	// crash is the last transient crash this call met: once one is known,
	// the pool is rolling and a resume that finds no free worker waits for
	// one with the same backoff.
	var crash string
	for attempt := 1; ; attempt++ {
		actor, resumed, err := w.resumeActorOnce(ctx, actorRef, token)
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

// workerLeavingCause names, as one of transientCrashCauses, why the worker
// the assignment names is leaving: it is draining, its record is gone, or its
// pod was replaced. It returns "" for a worker that keeps serving, or one it
// could not read.
func workerLeavingCause(ctx context.Context, st interface {
	GetWorker(context.Context, string) (*ateapi.Worker, error)
}, assignment *ateapi.WorkerAssignment) string {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), crashRecordTimeout)
	defer cancel()
	worker, err := st.GetWorker(readCtx, assignment.GetWorker().GetName())
	switch {
	case errors.Is(err, store.ErrNotFound):
		return crashMessageWorkerPodGone
	case err != nil:
		return ""
	case worker.GetStatus().GetState() == ateapi.WorkerState_WORKER_STATE_DRAINING:
		return crashMessageWorkerDraining
	case assignment.GetWorkerPodUid() != "" && worker.GetWorkerPodUid() != assignment.GetWorkerPodUid():
		return crashMessageWorkerPodGone
	}
	return ""
}

// staticWorker serves a worker already read to workerLeavingCause.
type staticWorker struct{ worker *ateapi.Worker }

func (s staticWorker) GetWorker(context.Context, string) (*ateapi.Worker, error) {
	return s.worker, nil
}
