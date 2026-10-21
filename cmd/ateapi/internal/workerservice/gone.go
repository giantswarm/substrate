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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/apivalidation"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// ListGoneActors names the requested Actor UIDs no stored Actor carries, so
// the calling atelet can reclaim their directories. The caller must be an
// atelet. Only a successful read of the store answers: a UID is named gone
// only when the store positively holds no Actor with it.
func (s *Server) ListGoneActors(ctx context.Context, req *ateapipb.ListGoneActorsRequest) (*ateapipb.ListGoneActorsResponse, error) {
	if _, err := ateletauth.Authenticate(ctx, s.ateletSPIFFEID); err != nil {
		return nil, err
	}
	if errs := apivalidation.ValidateListGoneActorsRequest(ctx, req); len(errs) > 0 {
		return nil, resources.ToAPIError(errs)
	}

	existing, err := s.store.ListExistingActorUIDs(ctx, req.GetActorUids())
	if err != nil {
		return nil, err
	}
	exists := make(map[string]bool, len(existing))
	for _, uid := range existing {
		exists[uid] = true
	}
	resp := &ateapipb.ListGoneActorsResponse{}
	for _, uid := range req.GetActorUids() {
		if !exists[uid] {
			resp.GoneActorUids = append(resp.GoneActorUids, uid)
		}
	}
	return resp, nil
}
