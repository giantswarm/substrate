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

package workerservice

import (
	"context"
	"log/slog"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/apivalidation"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// ReportActorExit crashes a running Actor whose sandbox exited, on the report
// of the Worker hosting it. The caller must be an atelet running on the
// Worker's node; that the Actor is the RUNNING incarnation assigned to the
// Worker is checked under the Actor's lease (controlapi.CrashExitedActor).
func (s *Server) ReportActorExit(ctx context.Context, req *ateapipb.ReportActorExitRequest) (*ateapipb.ReportActorExitResponse, error) {
	caller, err := ateletauth.Authenticate(ctx, s.ateletSPIFFEID)
	if err != nil {
		return nil, err
	}
	if errs := apivalidation.ValidateReportActorExitRequest(ctx, req); len(errs) > 0 {
		return nil, resources.ToAPIError(errs)
	}

	workerName := req.GetWorker().GetName()
	if err := s.authorizeWorker(ctx, caller, workerName, "sandbox exit report"); err != nil {
		return nil, err
	}

	actorRef := resources.ActorRefFromObjectRef(req.GetActor())
	actor, err := s.actors.CrashExitedActor(ctx, actorRef, req.GetActorUid(), workerName, req.GetReason())
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "Worker reported a sandbox exit; the actor is crashed",
		slog.String("worker", workerName),
		slog.String("actor", actorRef.String()),
		slog.String("actor_uid", req.GetActorUid()),
		slog.String("node", caller.NodeName),
		slog.String("reason", req.GetReason()))
	return &ateapipb.ReportActorExitResponse{Actor: actor}, nil
}
