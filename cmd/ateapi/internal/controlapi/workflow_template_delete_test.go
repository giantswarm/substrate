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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

var errStoreRefused = errors.New("the store refused the delete")

// refusingTemplateDeleteStore fails the template row delete with err while it
// is set and passes everything else through.
type refusingTemplateDeleteStore struct {
	store.Interface
	err error
}

func (s *refusingTemplateDeleteStore) DeleteActorTemplate(ctx context.Context, templateRef resources.ActorTemplateRef) (*ateapipb.ActorTemplate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.Interface.DeleteActorTemplate(ctx, templateRef)
}
