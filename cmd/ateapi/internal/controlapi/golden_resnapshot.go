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
	"slices"
	"time"

	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultGoldenResnapshotBudget bounds how long a resume waits for its
	// template's golden to be taken again: the reconciler's resync, a golden
	// actor's boot and the default warmup fit in it with room for a cold image
	// pull. A resume that outlasts it answers Unavailable; the take goes on.
	defaultGoldenResnapshotBudget = 2 * time.Minute

	// defaultGoldenResnapshotPoll is how often a waiting resume reads the
	// template for its new golden.
	defaultGoldenResnapshotPoll = 2 * time.Second
)

// borrowedGoldenSnapshot returns the snapshot the resume restores when the
// actor borrows it from its template's golden: created without a source tag,
// never suspended since, so its snapshot is still the golden tag's. It
// returns nil for any other actor.
func borrowedGoldenSnapshot(actor *ateapi.Actor) *ateapi.Snapshot {
	if actor.GetSourceTag() != nil {
		return nil
	}
	snap := snapshotToResume(actor.GetStatus())
	if snap.GetOwner() != ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG || snap.GetLocality() != "" || snap.GetActorTemplateUid() == "" {
		return nil
	}
	return snap
}

// ensureGoldenSourcePresent makes sure an actor that still starts from its
// template's golden has a golden to restore. A golden tag deleted while
// actors borrow its snapshot leaves them a snapshot whose objects are gone,
// whose restore would crash them. Such an actor has no guest state of its
// own: the golden is all it holds. So it is moved onto its template's current
// golden, as a create now would start it, and when the template has none the
// golden is taken again and the resume waits for it within
// goldenResnapshotBudget, answering Unavailable once that is spent. A golden
// the take fails on is refused.
func (w *ActorWorkflow) ensureGoldenSourcePresent(ctx context.Context, actorRef resources.ActorRef, actor *ateapi.Actor, tmpl *ateapi.ActorTemplate) (_ *ateapi.Actor, err error) {
	ctx, done := stepSpan(ctx, "EnsureGoldenSource")
	defer func() { err = done(err) }()

	switch state := actor.GetStatus().GetState(); {
	case state != ateapi.ActorState_ACTOR_STATE_SUSPENDED && state != ateapi.ActorState_ACTOR_STATE_RESUMING:
		markSkipped(ctx, "actor is not resuming from SUSPENDED")
		return actor, nil
	case tmpl.GetSnapshotConfig().GetGoldenSnapshotConfig().GetMode() == ateapi.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_DISABLED:
		markSkipped(ctx, "the template takes no golden")
		return actor, nil
	}
	borrowed := borrowedGoldenSnapshot(actor)
	if borrowed == nil {
		markSkipped(ctx, "actor restores no borrowed golden snapshot")
		return actor, nil
	}
	// The golden tag the snapshot was copied from is named after the template
	// it was captured under, in the template's atespace (see the reconciler).
	source := resources.TagRef{Atespace: tmpl.GetMetadata().GetAtespace(), Name: borrowed.GetActorTemplateUid()}
	if golden, err := w.completedTag(ctx, source); err != nil || golden.GetStatus().GetSnapshot().GetUuid() == borrowed.GetUuid() {
		if err == nil {
			markSkipped(ctx, "the borrowed golden snapshot is present")
		}
		return actor, err
	}
	slog.WarnContext(ctx, "The golden snapshot an actor starts from is gone; moving it onto its template's golden",
		slog.String("actor", actorRef.String()), slog.String("lostTag", source.String()),
		slog.String("ActorTemplate", resources.ActorTemplateRefFromActorTemplate(tmpl).String()))

	budget := w.goldenResnapshotBudget
	if budget == 0 {
		budget = defaultGoldenResnapshotBudget
	}
	poll := w.goldenResnapshotPoll
	if poll == 0 {
		poll = defaultGoldenResnapshotPoll
	}
	deadline := time.Now().Add(budget)
	for {
		golden, err := w.restorableGolden(ctx, tmpl, borrowed)
		if err != nil {
			return nil, err
		}
		if golden != nil {
			return w.rebaseOntoGolden(ctx, actorRef, actor, borrowed, golden)
		}
		if err := w.awaitableGolden(ctx, actorRef, tmpl); err != nil {
			return nil, err
		}
		if time.Now().Add(poll).After(deadline) {
			return nil, goldenResnapshotPending(actorRef, tmpl, budget)
		}
		select {
		case <-ctx.Done():
			return nil, goldenResnapshotPending(actorRef, tmpl, budget)
		case <-time.After(poll):
		}
		if tmpl, err = w.store.GetActorTemplate(ctx, resources.ActorTemplateRefFromActorTemplate(tmpl)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, apierror.FailedPrecondition("ActorTemplate of Actor %s was deleted", actorRef)
			}
			return nil, fmt.Errorf("while reading the ActorTemplate of actor %s: %w", actorRef, err)
		}
	}
}

// completedTag returns the tag when it exists and its snapshot's upload is
// recorded, nil otherwise.
func (w *ActorWorkflow) completedTag(ctx context.Context, tagRef resources.TagRef) (*ateapi.Tag, error) {
	tag, err := w.store.GetTag(ctx, tagRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("while reading golden tag %s: %w", tagRef, err)
	}
	if tagDurableSnapshotURI(tag, ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED) == "" {
		return nil, nil
	}
	return tag, nil
}

// restorableGolden returns the golden tag tmpl records when it can be
// restored from in place of lost: it exists, is complete, and holds another
// snapshot. Otherwise it returns nil.
func (w *ActorWorkflow) restorableGolden(ctx context.Context, tmpl *ateapi.ActorTemplate, lost *ateapi.Snapshot) (*ateapi.Tag, error) {
	ref := tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag()
	if ref.GetName() == "" {
		return nil, nil
	}
	tag, err := w.completedTag(ctx, resources.TagRefFromObjectRef(ref))
	if err != nil || tag == nil || tag.GetStatus().GetSnapshot().GetUuid() == lost.GetUuid() {
		return nil, err
	}
	return tag, nil
}

// awaitableGolden decides, for a template whose recorded golden cannot be
// restored from, whether the resume may wait for a new one. A failed golden
// is refused. A recorded golden tag that is gone or incomplete is released and
// the record cleared, which sends the template back to the reconciler to take
// its golden again. A golden being taken is waited for.
func (w *ActorWorkflow) awaitableGolden(ctx context.Context, actorRef resources.ActorRef, tmpl *ateapi.ActorTemplate) error {
	snapshotStatus := tmpl.GetStatus().GetGoldenSnapshotStatus()
	meta := tmpl.GetMetadata()
	switch {
	case snapshotStatus.GetErrorMessage() != "":
		return apierror.FailedPrecondition("the golden snapshot Actor %s starts from is gone and the golden of ActorTemplate %s/%s cannot be taken again: %s",
			actorRef, meta.GetAtespace(), meta.GetName(), snapshotStatus.GetErrorMessage())
	case snapshotStatus.GetGoldenTag() == nil:
		return nil
	}
	return w.requestGoldenResnapshot(ctx, tmpl)
}

// requestGoldenResnapshot sends tmpl back to the reconciler to take its
// golden again: the recorded golden tag, gone or incomplete, is deleted so no
// pass records it again, then the record is cleared under the observed
// template's version. A record a concurrent writer changed first is left to
// it; a conflict leaves the next read to send it back again.
func (w *ActorWorkflow) requestGoldenResnapshot(ctx context.Context, tmpl *ateapi.ActorTemplate) error {
	recorded := tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag()
	tmplRef := resources.ActorTemplateRefFromActorTemplate(tmpl)
	if _, err := w.DeleteTag(ctx, resources.TagRefFromObjectRef(recorded), store.DeletePreconditions{}); err != nil && apierror.Code(err) != codes.NotFound {
		return fmt.Errorf("while deleting the lost golden tag %s of ActorTemplate %s: %w", resources.TagRefFromObjectRef(recorded), tmplRef, err)
	}
	_, err := w.store.UpdateActorTemplate(ctx, tmplRef, store.PreconditionFrom(tmpl), func(dbTemplate *ateapi.ActorTemplate) error {
		snapshotStatus := dbTemplate.GetStatus().GetGoldenSnapshotStatus()
		if !proto.Equal(snapshotStatus.GetGoldenTag(), recorded) || snapshotStatus.GetErrorMessage() != "" {
			return errGoldenMovedOn
		}
		snapshotStatus.GoldenTag = nil
		snapshotStatus.SnapshotTime = nil
		return nil
	})
	switch {
	case err == nil:
		slog.InfoContext(ctx, "Sent an ActorTemplate whose golden is lost back to be taken again",
			slog.String("ActorTemplate", tmplRef.String()), slog.String("lostTag", resources.TagRefFromObjectRef(recorded).String()))
		return nil
	case errors.Is(err, errGoldenMovedOn), errors.Is(err, store.ErrVersionConflict):
		return nil
	default:
		return fmt.Errorf("while sending ActorTemplate %s back to take its golden again: %w", tmplRef, err)
	}
}

// errGoldenMovedOn aborts the clearing of a golden record a concurrent writer
// already changed.
var errGoldenMovedOn = errors.New("the golden record changed concurrently")

// rebaseOntoGolden replaces lost, the snapshot the actor borrowed, with
// golden's, as a create from the template now would start it: a new
// generation owned by the tag, stamped with the template it was built on.
func (w *ActorWorkflow) rebaseOntoGolden(ctx context.Context, actorRef resources.ActorRef, actor *ateapi.Actor, lost *ateapi.Snapshot, golden *ateapi.Tag) (*ateapi.Actor, error) {
	updated, err := w.store.UpdateActor(ctx, actorRef, store.PreconditionFrom(actor), func(toUpdate *ateapi.Actor) error {
		snap := proto.CloneOf(golden.GetStatus().GetSnapshot())
		snap.Generation = toUpdate.GetStatus().GetLastAssignedGeneration() + 1
		snap.Owner = ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG
		snap.ActorTemplateUid = golden.GetStatus().GetActorTemplateUid()
		toUpdate.Status.Snapshots = slices.DeleteFunc(toUpdate.Status.Snapshots, func(s *ateapi.Snapshot) bool { return s.GetUuid() == lost.GetUuid() })
		toUpdate.Status.Snapshots = append(toUpdate.Status.Snapshots, snap)
		toUpdate.Status.LastAssignedGeneration = snap.Generation
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrVersionConflict) {
			return nil, apierror.Aborted("concurrent update conflict, please retry")
		}
		return nil, fmt.Errorf("while moving actor %s onto its template's golden: %w", actorRef, err)
	}
	slog.InfoContext(ctx, "Moved an actor whose golden snapshot is gone onto its template's golden",
		slog.String("actor", actorRef.String()), slog.String("goldenTag", resources.TagRefFromTag(golden).String()))
	return updated, nil
}

// goldenResnapshotPending answers a resume whose template's golden is being
// taken again and was not complete within the budget: a retry outlives it.
func goldenResnapshotPending(actorRef resources.ActorRef, tmpl *ateapi.ActorTemplate, budget time.Duration) error {
	meta := tmpl.GetMetadata()
	return apierror.Unavailable("the golden snapshot Actor %s starts from is gone and the golden of ActorTemplate %s/%s is being taken again, not complete within %s; retry",
		actorRef, meta.GetAtespace(), meta.GetName(), budget)
}
