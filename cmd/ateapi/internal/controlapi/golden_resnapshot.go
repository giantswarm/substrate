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
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/objectstore"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

// reasonGoldenSnapshotUnavailable is the ErrorInfo reason of a resume refused
// because the golden snapshot the actor still starts from is gone and its
// template's golden could not be taken again: the actor has nothing to restore.
const reasonGoldenSnapshotUnavailable = "GOLDEN_SNAPSHOT_UNAVAILABLE"

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

// borrowsGoldenSnapshot reports whether the resume restores a snapshot the
// actor borrows from a golden: its source is its external snapshot, not a
// pause, and that snapshot lives under a prefix of the golden atespace rather
// than the actor's own. Only an actor never suspended since its create does;
// its first suspend writes a snapshot of its own.
func borrowsGoldenSnapshot(actor *ateapipb.Actor, src resumeSnapshotSource) bool {
	if src.SnapshotURI.IsZero() || actor.GetStatus().GetLocalSnapshot() != nil || durablePauseCopy(actor) != nil {
		return false
	}
	return !src.SnapshotURI.OwnedBy(actorSnapshotOwner(actor)) && src.SnapshotURI.Atespace() == resources.GoldenActorAtespace
}

// ensureGoldenSourcePresent makes sure an actor that still starts from its
// template's golden has a golden to restore. A golden tag deleted while
// actors borrow its snapshot (by hand, or by the reconciler replacing a golden
// taken in an older snapshot layout) leaves them a snapshot URI with no
// objects, whose restore would crash them. Such an actor has no guest state
// of its own: the golden is all it holds. So it is moved onto its template's
// current golden, as a create now would start it, and when the template has
// none the golden is taken again and the resume waits for it within
// goldenResnapshotBudget, answering Unavailable once that is spent. A golden
// the take fails on for good is refused as not resumable.
func (w *ActorWorkflow) ensureGoldenSourcePresent(ctx context.Context, actorRef resources.ActorRef, actor *ateapipb.Actor, tmpl *ateapipb.ActorTemplate, src resumeSnapshotSource) (_ *ateapipb.Actor, _ resumeSnapshotSource, err error) {
	ctx, done := stepSpan(ctx, "EnsureGoldenSource")
	defer func() { err = done(err) }()

	switch state := actor.GetStatus().GetState(); {
	case state != ateapipb.ActorState_ACTOR_STATE_SUSPENDED && state != ateapipb.ActorState_ACTOR_STATE_RESUMING:
		markSkipped(ctx, "actor is not resuming from SUSPENDED")
		return actor, src, nil
	case !borrowsGoldenSnapshot(actor, src):
		markSkipped(ctx, "actor restores no borrowed golden snapshot")
		return actor, src, nil
	case w.objectStore == nil:
		markSkipped(ctx, "no object store configured")
		return actor, src, nil
	}
	present, err := objectstore.HasObjects(ctx, w.objectStore, src.SnapshotURI.Prefix())
	if err != nil {
		return nil, src, fmt.Errorf("while checking the golden snapshot actor %s starts from: %w", actorRef, err)
	}
	if present {
		markSkipped(ctx, "the borrowed golden snapshot is present")
		return actor, src, nil
	}
	lost := src.SnapshotURI
	slog.LogAttrs(ctx, slog.LevelWarn, "The golden snapshot an actor starts from is gone; moving it onto its template's golden",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.String("snapshot_uri", lost.String()),
			slog.String("ActorTemplate", resources.ActorTemplateRefFromActorTemplate(tmpl).String()))...)

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
		golden, err := w.restorableGolden(ctx, tmpl, lost)
		if err != nil {
			return nil, src, err
		}
		if golden != nil {
			return w.rebaseOntoGolden(ctx, actorRef, actor, tmpl, golden)
		}
		if err := w.awaitableGolden(ctx, actorRef, tmpl); err != nil {
			return nil, src, err
		}
		if time.Now().Add(poll).After(deadline) {
			return nil, src, goldenResnapshotPending(actorRef, tmpl, budget)
		}
		select {
		case <-ctx.Done():
			return nil, src, goldenResnapshotPending(actorRef, tmpl, budget)
		case <-time.After(poll):
		}
		if tmpl, err = w.store.GetActorTemplate(ctx, resources.ActorTemplateRefFromActorTemplate(tmpl)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, src, apierror.FailedPrecondition("ActorTemplate of Actor %s was deleted", actorRef)
			}
			return nil, src, fmt.Errorf("while reading the ActorTemplate of actor %s: %w", actorRef, err)
		}
	}
}

// restorableGolden returns the golden tag tmpl records when it can be
// restored from: it exists, is complete, is not the lost one, and its
// snapshot's objects are present. Otherwise it returns nil.
func (w *ActorWorkflow) restorableGolden(ctx context.Context, tmpl *ateapipb.ActorTemplate, lost resources.SnapshotURI) (*ateapipb.Tag, error) {
	ref := tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag()
	if ref.GetName() == "" {
		return nil, nil
	}
	tag, err := w.store.GetTag(ctx, resources.TagRefFromObjectRef(ref))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("while reading golden tag %s: %w", resources.TagRefFromObjectRef(ref), err)
	}
	snapshot := tag.GetStatus().GetSnapshot()
	if snapshot.GetSnapshotUri() == "" {
		return nil, nil
	}
	uri, err := resources.ParseSnapshotURI(snapshot.GetSnapshotUri())
	if err != nil {
		return nil, apierror.DataLoss("golden tag %s snapshot: %v", resources.TagRefFromObjectRef(ref), err)
	}
	if uri.Prefix() == lost.Prefix() {
		return nil, nil
	}
	present, err := objectstore.HasObjects(ctx, w.objectStore, uri.Prefix())
	if err != nil {
		return nil, fmt.Errorf("while checking golden tag %s: %w", resources.TagRefFromObjectRef(ref), err)
	}
	if !present {
		return nil, nil
	}
	return tag, nil
}

// awaitableGolden decides, for a template whose recorded golden cannot be
// restored from, whether the resume may wait for a new one. A golden failed
// for good is refused as not resumable. A recorded golden tag that is gone, or
// whose snapshot was collected, is released and the record cleared, which
// sends the template back to the reconciler to take its golden again. A
// golden being taken, migrated or taken again is waited for.
func (w *ActorWorkflow) awaitableGolden(ctx context.Context, actorRef resources.ActorRef, tmpl *ateapipb.ActorTemplate) error {
	snapshotStatus := tmpl.GetStatus().GetGoldenSnapshotStatus()
	meta := tmpl.GetMetadata()
	switch {
	case snapshotStatus.GetErrorMessage() != "" && goldenTemplateDone(tmpl):
		return apierror.NotResumable(reasonGoldenSnapshotUnavailable,
			"the golden snapshot Actor %s starts from is gone and the golden of ActorTemplate %s/%s cannot be taken again: %s",
			actorRef, meta.GetAtespace(), meta.GetName(), snapshotStatus.GetErrorMessage())
	case snapshotStatus.GetErrorMessage() != "", legacyGoldenSnapshot(snapshotStatus), snapshotStatus.GetGoldenTag() == nil:
		return nil
	}
	return w.requestGoldenResnapshot(ctx, tmpl)
}

// requestGoldenResnapshot sends tmpl back to the reconciler to take its
// golden again: the recorded golden tag, gone or holding a collected snapshot,
// is deleted so no pass records it again, then the record is cleared under the
// observed template's version. A record a concurrent writer changed first is
// left to it; a conflict leaves the next read to send it back again.
func (w *ActorWorkflow) requestGoldenResnapshot(ctx context.Context, tmpl *ateapipb.ActorTemplate) error {
	recorded := tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag()
	tmplRef := resources.ActorTemplateRefFromActorTemplate(tmpl)
	if _, err := w.DeleteTag(ctx, resources.TagRefFromObjectRef(recorded), store.DeletePreconditions{}); err != nil && apierror.Code(err) != codes.NotFound {
		return fmt.Errorf("while deleting the lost golden tag %s of ActorTemplate %s: %w", resources.TagRefFromObjectRef(recorded), tmplRef, err)
	}
	_, err := w.store.UpdateActorTemplate(ctx, tmplRef, store.PreconditionFrom(tmpl), func(dbTemplate *ateapipb.ActorTemplate) error {
		snapshotStatus := dbTemplate.GetStatus().GetGoldenSnapshotStatus()
		if !proto.Equal(snapshotStatus.GetGoldenTag(), recorded) || snapshotStatus.GetErrorMessage() != "" {
			return errGoldenMovedOn
		}
		snapshotStatus.GoldenTag = nil
		snapshotStatus.SnapshotLayout = 0
		snapshotStatus.TakeGoldenSnapshotAt = nil
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

// rebaseOntoGolden points the actor at golden, its template's golden tag, in
// place of the lost snapshot it borrowed: it borrows the new golden as a
// create from the template now would, stamped with the template it was built
// on.
func (w *ActorWorkflow) rebaseOntoGolden(ctx context.Context, actorRef resources.ActorRef, actor *ateapipb.Actor, tmpl *ateapipb.ActorTemplate, golden *ateapipb.Tag) (*ateapipb.Actor, resumeSnapshotSource, error) {
	var src resumeSnapshotSource
	snapshot := golden.GetStatus().GetSnapshot()
	if err := validateGoldenSnapshotScope(snapshot); err != nil {
		return nil, src, err
	}
	updated, err := w.store.UpdateActor(ctx, actorRef, store.PreconditionFrom(actor), func(toUpdate *ateapipb.Actor) error {
		toUpdate.Status.ExternalSnapshot = proto.CloneOf(snapshot)
		toUpdate.Status.ExternalSnapshot.ActorTemplateUid = golden.GetStatus().GetActorTemplateUid()
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrVersionConflict) {
			return nil, src, apierror.Aborted("concurrent update conflict, please retry")
		}
		return nil, src, fmt.Errorf("while moving actor %s onto its template's golden: %w", actorRef, err)
	}
	ext := updated.GetStatus().GetExternalSnapshot()
	if src.SnapshotURI, err = resources.ParseSnapshotURI(ext.GetSnapshotUri()); err != nil {
		return nil, src, apierror.DataLoss("Actor %s snapshot: %v", actorRef, err)
	}
	src.Scope = ext.GetContentScope()
	capturedUnder := ext.GetActorTemplateUid()
	src.TemplateReplaced = capturedUnder != "" && capturedUnder != tmpl.GetMetadata().GetUid()
	slog.LogAttrs(ctx, slog.LevelInfo, "Moved an actor whose golden snapshot is gone onto its template's golden",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.String("goldenTag", resources.TagRefFromTag(golden).String()),
			slog.String("snapshot_uri", ext.GetSnapshotUri()))...)
	return updated, src, nil
}

// goldenResnapshotPending answers a resume whose template's golden is being
// taken again and was not complete within the budget: a retry outlives it.
func goldenResnapshotPending(actorRef resources.ActorRef, tmpl *ateapipb.ActorTemplate, budget time.Duration) error {
	meta := tmpl.GetMetadata()
	return apierror.Unavailable("the golden snapshot Actor %s starts from is gone and the golden of ActorTemplate %s/%s is being taken again, not complete within %s; retry",
		actorRef, meta.GetAtespace(), meta.GetName(), budget)
}
