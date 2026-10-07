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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
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

// goldenSnapshotAwaitingMigration refuses, for the moment, a create on a
// template whose golden snapshot a release before golden tags recorded: the
// ActorTemplate reconciler migrates it into the golden tag the new actor
// starts from on its next pass, so the refusal is one a retry outlives.
func goldenSnapshotAwaitingMigration(actorTemplate *ateapipb.ActorTemplate) error {
	meta := actorTemplate.GetMetadata()
	return status.Errorf(codes.Unavailable, "the golden snapshot of ActorTemplate %s/%s was recorded before golden tags and awaits its migration into one; retry", meta.GetAtespace(), meta.GetName())
}
