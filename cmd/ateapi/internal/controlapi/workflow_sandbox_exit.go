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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// crashMessageSandboxExited starts the crash recorded for a running actor
// whose worker reported that its sandbox exited; the worker's observation
// follows it.
const crashMessageSandboxExited = "sandbox exited while the actor was running"

// CrashExitedActor crashes a RUNNING actor whose sandbox the worker it is
// assigned to reported exited (WorkerService.ReportActorExit), so a client sees
// the lost workload as a crash it can revert instead of an actor that answers
// nothing. The crash frees the worker, as for any other crash.
//
// Checked under the actor's lease, against the stored record: the actor must
// be the incarnation actorUID names and be assigned to workerName, or the
// report is NotFound; it must be RUNNING, or FailedPrecondition, since an
// operation that already moved it on owns its fate and fails on the exited
// sandbox itself. A lease another operation holds is Aborted, for the worker
// to retry.
func (w *ActorWorkflow) CrashExitedActor(ctx context.Context, actorRef resources.ActorRef, actorUID, workerName, reason string) (*ateapipb.Actor, error) {
	leaseCtx, lease, err := w.acquireActorLease(ctx, actorRef)
	if err != nil {
		return nil, err
	}
	defer lease.Close()

	actor, err := w.store.GetActor(leaseCtx, actorRef)
	if errors.Is(err, store.ErrNotFound) {
		return nil, apierror.NotFound("Actor %s not found", actorRef)
	}
	if err != nil {
		return nil, fmt.Errorf("while getting actor %s: %w", actorRef, err)
	}
	if actor.GetMetadata().GetUid() != actorUID ||
		actor.GetStatus().GetWorkerAssignment().GetWorker().GetName() != workerName {
		return nil, apierror.NotFound("Actor %s not found", actorRef)
	}
	if state := actor.GetStatus().GetState(); state != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		return nil, apierror.FailedPrecondition("Actor %s is %s, not RUNNING", actorRef, state)
	}

	slog.LogAttrs(leaseCtx, slog.LevelError, "Setting Actor to crashed: its worker reported that its sandbox exited",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.String("worker", workerName), slog.String("reason", reason))...)
	if err := crashActor(leaseCtx, w.store, actorRef, ateattr.OperationUnknown, crashMessageSandboxExited+": "+reason); err != nil {
		return nil, err
	}
	return w.store.GetActor(leaseCtx, actorRef)
}
