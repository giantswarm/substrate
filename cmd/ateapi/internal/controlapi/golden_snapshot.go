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

// legacyGoldenSnapshot reports whether the status still holds the golden
// snapshot a release before golden tags recorded. Field 1 of
// GoldenSnapshotStatus was then an ExternalSnapshot; read as the ObjectRef it
// is now, its snapshot_uri lands in atespace and the name stays empty. A
// golden tag the reconciler records always has a name.
func legacyGoldenSnapshot(snapshotStatus *ateapipb.GoldenSnapshotStatus) bool {
	ref := snapshotStatus.GetGoldenTag()
	return ref != nil && ref.GetName() == ""
}

// goldenSnapshotAwaitingMigration refuses, for the moment, a resume or a create
// on a template whose golden snapshot a release before golden tags recorded:
// the ActorTemplate reconciler migrates it into the golden tag the caller needs
// on its next pass, so the refusal is one a retry outlives.
func goldenSnapshotAwaitingMigration(actorTemplate *ateapipb.ActorTemplate) error {
	meta := actorTemplate.GetMetadata()
	return status.Errorf(codes.Unavailable, "the golden snapshot of ActorTemplate %s/%s was recorded before golden tags and awaits its migration into one; retry", meta.GetAtespace(), meta.GetName())
}

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
