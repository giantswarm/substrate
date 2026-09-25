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
	"fmt"
	"log/slog"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	epb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The AIP-193 ErrorInfo (https://google.aip.dev/193) a resume refusal carries
// when no retry can outlive it. A router and a client read the reason and the
// directive off the wire and act on them: the router answers at once instead
// of parking the request, the client starts over instead of retrying.
const (
	// errorInfoDomain identifies Agent Substrate as the source of the error.
	errorInfoDomain = "substrate.dev"

	// ReasonGoldenSnapshotUnavailable marks a resume that restores an actor's
	// data onto its ActorTemplate's golden snapshot when the template has no
	// usable one: the golden tag is gone, incomplete, or another template's.
	// Retrying the resume cannot bring it back; the actor itself is left as it
	// is.
	ReasonGoldenSnapshotUnavailable = "GOLDEN_SNAPSHOT_UNAVAILABLE"

	// MetadataKeyResumable marks (in ErrorInfo.Metadata, value "false") a
	// resume refusal that no retry outlives: the actor cannot be resumed as it
	// is. The actor is not crashed by it.
	MetadataKeyResumable = "resumable"
)

// goldenSnapshotUnavailable refuses a resume that restores the actor's data onto
// its ActorTemplate's golden snapshot when the template has no usable one. The
// refusal carries ReasonGoldenSnapshotUnavailable so a caller can tell it from
// the FailedPrecondition of an actor in transition, which a retry outlives, and
// the not-resumable directive a router and a client act on: no retry outlives
// this one. The actor keeps its state for a later recovery.
func goldenSnapshotUnavailable(ctx context.Context, actorTemplate *ateapipb.ActorTemplate, cause string) error {
	meta := actorTemplate.GetMetadata()
	message := fmt.Sprintf("%s for %s/%s: the actor cannot be resumed; start a new actor", cause, meta.GetAtespace(), meta.GetName())
	st, err := status.New(codes.FailedPrecondition, message).WithDetails(&epb.ErrorInfo{
		Domain:   errorInfoDomain,
		Reason:   ReasonGoldenSnapshotUnavailable,
		Metadata: map[string]string{MetadataKeyResumable: "false"},
	})
	if err != nil {
		// WithDetails on an ErrorInfo does not fail; should it, the caller
		// still gets the refusal, without the directive a router parks on.
		slog.ErrorContext(ctx, "Failed to attach ErrorInfo to the golden snapshot refusal", slog.Any("err", err))
		return status.Error(codes.FailedPrecondition, message)
	}
	return st.Err()
}
