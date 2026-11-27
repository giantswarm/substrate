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

	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/protobuf/proto"
)

// fencingTokenStale reports whether token is older than recorded: a lower
// generation, or the same generation under another holder. A nil token is
// never stale.
func fencingTokenStale(token, recorded *ateapi.FencingToken) bool {
	if token == nil || recorded == nil {
		return false
	}
	if token.GetGeneration() != recorded.GetGeneration() {
		return token.GetGeneration() < recorded.GetGeneration()
	}
	return token.GetHolder() != recorded.GetHolder()
}

// fencingTokenNewer reports whether token supersedes recorded, so the Actor
// must record it.
func fencingTokenNewer(token, recorded *ateapi.FencingToken) bool {
	if token == nil {
		return false
	}
	return recorded == nil || token.GetGeneration() > recorded.GetGeneration()
}

// staleFencingTokenError is the FailedPrecondition a request with a
// superseded fencing token is rejected with.
func staleFencingTokenError(actorRef resources.ActorRef, token, recorded *ateapi.FencingToken) error {
	return apierror.FailedPrecondition("fencing token (holder %q, generation %d) for Actor %s is superseded by (holder %q, generation %d)",
		token.GetHolder(), token.GetGeneration(), actorRef, recorded.GetHolder(), recorded.GetGeneration())
}

// ensureFencingTokenAdmitted rejects a request whose fencing token is older
// than the newest the Actor recorded, and records a newer one before the
// workflow acts, so a late request of a superseded holder can no longer
// land. A request without a token is admitted unchanged. Runs under the
// Actor's lease, which orders it against every other lifecycle workflow.
func (w *ActorWorkflow) ensureFencingTokenAdmitted(ctx context.Context, actorRef resources.ActorRef, actor *ateapi.Actor, token *ateapi.FencingToken) (_ *ateapi.Actor, err error) {
	if token == nil {
		return actor, nil
	}
	ctx, done := stepSpan(ctx, "AdmitFencingToken")
	defer func() { err = done(err) }()

	recorded := actor.GetStatus().GetFencingToken()
	if fencingTokenStale(token, recorded) {
		return nil, staleFencingTokenError(actorRef, token, recorded)
	}
	if !fencingTokenNewer(token, recorded) {
		markSkipped(ctx, "fencing token already recorded")
		return actor, nil
	}
	storedActor, err := w.store.UpdateActor(ctx, actorRef, store.PreconditionFrom(actor), func(toUpdate *ateapi.Actor) error {
		toUpdate.Status.FencingToken = proto.CloneOf(token)
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrVersionConflict) {
			return nil, apierror.Aborted("concurrent update conflict, please retry")
		}
		return nil, err
	}
	return storedActor, nil
}
